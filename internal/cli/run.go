package cli

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/jsonwire"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

const maxReportBytes = 32 * 1024 * 1024

// Run returns 0 on success, 1 on execution failure, 2 on invalid local
// arguments/configuration, and optionally 3 when a dry-run detects changes.
func Run(ctx context.Context, args []string, options Options) int {
	if options.Stdout == nil {
		options.Stdout = os.Stdout
	}
	if options.Stderr == nil {
		options.Stderr = os.Stderr
	}
	if options.LookupEnv == nil {
		options.LookupEnv = os.LookupEnv
	}
	fail := func(code int, err error) int {
		_, _ = fmt.Fprintln(options.Stderr, err)
		return code
	}
	if len(args) == 0 {
		return fail(2, errors.New("usage: flux-repository-discovery <serve|validate|dry-run|version>"))
	}
	command := args[0]
	if command != "serve" && command != "validate" && command != "dry-run" && command != "version" {
		return fail(2, fmt.Errorf("unknown command %q", command))
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var configPath, credentialsPath, against, output string
	var detailed bool
	if command != "version" {
		configPath, _ = options.LookupEnv("FRD_CONFIG")
		flags.StringVar(&configPath, "config", configPath, "configuration file (or FRD_CONFIG)")
	}
	if command == "serve" {
		credentialsPath, _ = options.LookupEnv("FRD_CREDENTIALS_FILE")
		flags.StringVar(&credentialsPath, "credentials-file", credentialsPath, "server credential references (or FRD_CREDENTIALS_FILE)")
	}
	if command == "dry-run" {
		flags.StringVar(&against, "against", "", "running service URL")
		flags.StringVar(&output, "output", "text", "text or json")
		flags.BoolVar(&detailed, "detailed-exitcode", false, "exit 3 when changes are found")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			flags.SetOutput(options.Stdout)
			flags.PrintDefaults()
			return 0
		}
		return fail(2, err)
	}
	if flags.NArg() != 0 {
		return fail(2, errors.New("unexpected positional arguments"))
	}
	if command == "version" {
		version := options.Version
		if version == "" {
			version = "dev"
		}
		if _, err := fmt.Fprintln(options.Stdout, version); err != nil {
			return fail(1, err)
		}
		return 0
	}
	if configPath == "" {
		return fail(2, errors.New("--config or FRD_CONFIG is required"))
	}
	cfg, err := readConfig(configPath)
	if err != nil {
		return fail(2, err)
	}
	switch command {
	case "validate":
		if _, err := fmt.Fprintln(options.Stdout, "Configuration is valid."); err != nil {
			return fail(1, err)
		}
	case "serve":
		if credentialsPath == "" {
			return fail(2, errors.New("--credentials-file or FRD_CREDENTIALS_FILE is required"))
		}
		if options.Serve == nil {
			return fail(1, errors.New("server runtime is unavailable"))
		}
		env := make(map[string]string)
		for _, name := range []string{"FRD_LISTEN", "FRD_SCAN_INTERVAL", "FRD_SCAN_TIMEOUT"} {
			if value, ok := options.LookupEnv(name); ok {
				env[name] = value
			}
		}
		if err := options.Serve(ctx, ServeOptions{Config: cfg, CredentialsFile: credentialsPath, Env: env}); err != nil {
			return fail(1, err)
		}
	case "dry-run":
		endpoint, err := previewEndpoint(against)
		if err != nil {
			return fail(2, err)
		}
		if output != "text" && output != "json" {
			return fail(2, errors.New("--output must be text or json"))
		}
		report, err := requestPreview(ctx, endpoint, cfg, options.HTTPClient)
		if err != nil {
			return fail(1, err)
		}
		var data []byte
		if output == "json" {
			data, err = jsonwire.Marshal(report)
			data = append(data, '\n')
		} else {
			data = renderReport(report)
		}
		if err != nil {
			return fail(1, err)
		}
		if _, err := options.Stdout.Write(data); err != nil {
			return fail(1, err)
		}
		if detailed && report.HasChanges {
			return 3
		}
	}
	return 0
}

func readConfig(path string) (config.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return config.Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, config.MaxBytes+1))
	if err != nil {
		return config.Config{}, fmt.Errorf("read configuration: %w", err)
	}
	return config.Decode(data)
}

func previewEndpoint(value string) (string, error) {
	endpoint, err := url.Parse(value)
	if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return "", errors.New("--against must be an HTTP(S) service URL without user info, query or fragment")
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/api/v1/dry-run"
	endpoint.RawPath = ""
	return endpoint.String(), nil
}

func requestPreview(ctx context.Context, endpoint string, cfg config.Config, client *http.Client) (service.Report, error) {
	data, err := jsonwire.Marshal(struct {
		Config config.Document `json:"config"`
	}{cfg.Document()})
	if err != nil {
		return service.Report{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return service.Report{}, errors.New("could not construct dry-run request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	// A redirect must not send a candidate configuration to a different service.
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := copyClient.Do(request)
	if err != nil {
		return service.Report{}, errors.New("dry-run request failed or timed out")
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, maxReportBytes+1))
	if err != nil || len(body) > maxReportBytes {
		return service.Report{}, errors.New("could not read bounded dry-run report")
	}
	if response.StatusCode != http.StatusOK {
		var envelope struct {
			Error service.Error `json:"error"`
		}
		if json.Unmarshal(body, &envelope) == nil && envelope.Error.Code != "" {
			return service.Report{}, fmt.Errorf("dry-run HTTP %d: %s", response.StatusCode, envelope.Error.Error())
		}
		return service.Report{}, fmt.Errorf("dry-run HTTP %d", response.StatusCode)
	}
	var report service.Report
	if err := json.Unmarshal(body, &report); err != nil || !validReport(report) {
		return service.Report{}, errors.New("service returned an invalid or unsupported dry-run report")
	}
	return report, nil
}

func validReport(report service.Report) bool {
	if report.Version != 1 || report.BaseRevision == "" || report.CandidateRevision == "" || report.ObservedAt.IsZero() || report.Filters == nil {
		return false
	}
	for _, diff := range report.Filters {
		if !validDiff(diff) {
			return false
		}
	}
	for _, drift := range report.Drift {
		if drift.Available && !validDiff(drift.Diff) {
			return false
		}
	}
	return true
}

func validDiff(diff service.FilterDiff) bool {
	if diff.Kind != "added" && diff.Kind != "removed" && diff.Kind != "modified" && diff.Kind != "unchanged" {
		return false
	}
	return diff.BeforeCount >= 0 && diff.AfterCount >= 0 && diff.Unchanged >= 0 && diff.Inputs != nil &&
		diff.AfterCount == len(diff.Inputs) &&
		diff.BeforeCount == len(diff.Removed)+len(diff.Changed)+diff.Unchanged &&
		diff.AfterCount == len(diff.Added)+len(diff.Changed)+diff.Unchanged
}
