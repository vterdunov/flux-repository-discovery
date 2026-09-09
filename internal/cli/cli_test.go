package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/cli"
	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

const configYAML = `version: 1
server: {listen: ':8080'}
scan: {interval: 10m, timeout: 2m}
sources: {company: {owners: [acme], credential: token}}
filters: {apps: {sources: [company], include: [{topics: {all: [gitops]}}]}}
`

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func writeConfig(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "candidate.yaml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func env(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) { value, ok := values[key]; return value, ok }
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
func options(stdout, stderr *bytes.Buffer) cli.Options {
	return cli.Options{Stdout: stdout, Stderr: stderr, LookupEnv: env(nil), Version: "v1.2.3-test", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unexpected network request") })}}
}
func successReport() service.Report {
	return service.Report{Version: 1, BaseRevision: "base-revision", CandidateRevision: "candidate-revision", BaseGeneration: 7, ObservedAt: time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC), HasChanges: true,
		ConfigChanges: []service.Change{{Operation: "replace", Path: "scan.interval", Before: json.RawMessage(`"10m0s"`), After: json.RawMessage(`"5m0s"`)}, {Operation: "remove", Path: "filters.legacy", Before: json.RawMessage(`{"sources":["company"],"include":[{"nameRegex":"legacy"}]}`)}},
		Filters: map[config.FilterName]service.FilterDiff{
			"apps":   {Kind: "modified", BeforeCount: 2, AfterCount: 2, Added: []discovery.Input{{ID: "3", FullName: "acme/new", Owner: "acme", Name: "new"}}, Removed: []discovery.Input{{ID: "2", FullName: "acme/old", Owner: "acme", Name: "old"}}, Changed: []service.InputChange{{Before: discovery.Input{ID: "1", FullName: "acme/before", Owner: "acme", Name: "before"}, After: discovery.Input{ID: "1", FullName: "acme/after", Owner: "acme", Name: "after"}}}, Inputs: []discovery.Input{{ID: "1", FullName: "acme/after", Owner: "acme", Name: "after"}, {ID: "3", FullName: "acme/new", Owner: "acme", Name: "new"}}},
			"legacy": {Kind: "removed", BeforeCount: 1, Removed: []discovery.Input{{ID: "9", FullName: "acme/legacy", Owner: "acme", Name: "legacy"}}, Inputs: []discovery.Input{}},
		}, Drift: map[config.FilterName]service.Drift{"apps": {Available: false}}, Overrides: map[string]string{"FRD_SCAN_TIMEOUT": "30s"}}
}

func TestVersionAndValidateAreLocalOnly(t *testing.T) {
	path := writeConfig(t, configYAML)
	for _, args := range [][]string{{"version"}, {"validate", "--config", path}} {
		var stdout, stderr bytes.Buffer
		code := cli.Run(context.Background(), args, options(&stdout, &stderr))
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("%v: code=%d stdout=%s stderr=%s", args, code, &stdout, &stderr)
		}
		if args[0] == "version" && !strings.Contains(stdout.String(), "v1.2.3-test") {
			t.Fatalf("version missing: %s", &stdout)
		}
	}
}

func TestConfigPathFlagOverridesEnvAndInvalidConfigIsExitTwo(t *testing.T) {
	good := writeConfig(t, configYAML)
	bad := writeConfig(t, "version: 99\n")
	var stdout, stderr bytes.Buffer
	opts := options(&stdout, &stderr)
	opts.LookupEnv = env(map[string]string{"FRD_CONFIG": bad})
	if code := cli.Run(context.Background(), []string{"validate", "--config", good}, opts); code != 0 {
		t.Fatalf("flag did not win over env: code=%d %s", code, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	opts.LookupEnv = env(map[string]string{"FRD_CONFIG": good})
	if code := cli.Run(context.Background(), []string{"validate"}, opts); code != 0 {
		t.Fatalf("config env ignored: %d %s", code, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	opts.LookupEnv = env(map[string]string{"FRD_CONFIG": bad})
	if code := cli.Run(context.Background(), []string{"validate"}, opts); code != 2 || stderr.Len() == 0 {
		t.Fatalf("invalid config exit=%d stderr=%s", code, &stderr)
	}
}

func TestInvalidArgumentsNeverReachNetworkOrServe(t *testing.T) {
	path := writeConfig(t, configYAML)
	for _, args := range [][]string{
		{}, {"unknown"}, {"validate"}, {"validate", "--config", path, "extra"}, {"validate", "--config", path, "--unknown"},
		{"version", "extra"}, {"version", "--unknown"}, {"dry-run", "--config", path},
		{"dry-run", "--config", path, "--against", "not-a-url"}, {"dry-run", "--config", path, "--against", "file:///etc/passwd"},
		{"dry-run", "--config", path, "--against", "https://discovery.example", "--output", "xml"},
		{"dry-run", "--config", path, "--against", "https://discovery.example", "extra"},
		{"serve", "--config", path}, {"serve", "--config", path, "--credentials-file", "credentials.yaml", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opts := options(&stdout, &stderr)
			opts.Serve = func(context.Context, cli.ServeOptions) error { t.Fatal("invalid args started server"); return nil }
			code := cli.Run(context.Background(), args, opts)
			if code != 2 || stderr.Len() == 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
		})
	}
}

func TestDryRunSendsDeclaredConfigToServerAndPrintsOnlyJSON(t *testing.T) {
	path := writeConfig(t, configYAML)
	want := successReport()
	wire, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	var stdout, stderr bytes.Buffer
	opts := options(&stdout, &stderr)
	opts.LookupEnv = env(map[string]string{"FRD_LISTEN": ":6666", "FRD_SCAN_INTERVAL": "1s", "FRD_SCAN_TIMEOUT": "invalid", "FRD_SECRET": "must-not-send"})
	opts.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPost || r.URL.String() != "https://discovery.example/api/v1/dry-run" {
			t.Fatalf("request = %s %s", r.Method, r.URL)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			t.Fatal("missing JSON content type")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "must-not-send") || strings.Contains(string(body), ":6666") {
			t.Fatal("local server env or secrets leaked into candidate")
		}
		var envelope struct {
			Config json.RawMessage `json:"config"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Decode(envelope.Config)
		if err != nil {
			t.Fatal(err)
		}
		if time.Duration(cfg.Scan().Interval) != 10*time.Minute || time.Duration(cfg.Scan().Timeout) != 2*time.Minute || cfg.Server().Listen != ":8080" {
			t.Fatalf("CLI applied local env to candidate: %#v", cfg)
		}
		return response(200, string(wire)), nil
	})}
	code := cli.Run(context.Background(), []string{"dry-run", "--config", path, "--against", "https://discovery.example/", "--output", "json"}, opts)
	if code != 0 || calls != 1 || stderr.Len() != 0 {
		t.Fatalf("code=%d calls=%d stderr=%s", code, calls, &stderr)
	}
	var got service.Report
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not pure JSON: %v: %s", err, &stdout)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON report lost fields: %#v", got)
	}
}

func TestDetailedExitCodeAndHumanReadableFullDiff(t *testing.T) {
	path := writeConfig(t, configYAML)
	for _, hasChanges := range []bool{false, true} {
		report := successReport()
		report.HasChanges = hasChanges
		wire, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		opts := options(&stdout, &stderr)
		opts.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, string(wire)), nil })}
		code := cli.Run(context.Background(), []string{"dry-run", "--config", path, "--against", "https://discovery.example", "--detailed-exitcode"}, opts)
		wantCode := 0
		if hasChanges {
			wantCode = 3
		}
		if code != wantCode || stderr.Len() != 0 {
			t.Fatalf("hasChanges=%v code=%d stderr=%s", hasChanges, code, &stderr)
		}
		for _, text := range []string{"scan.interval", "10m0s", "5m0s", "apps", "acme/new", "acme/old", "acme/before", "acme/after", "/inputs/legacy", "404", "FRD_SCAN_TIMEOUT", "30s"} {
			if !strings.Contains(stdout.String(), text) {
				t.Errorf("human report missing %q: %s", text, &stdout)
			}
		}
	}
}

func TestDryRunFailuresAreStderrAndExitOne(t *testing.T) {
	path := writeConfig(t, configYAML)
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"upstream unavailable", 503, `{"error":{"code":"source_unavailable","message":"GitHub unavailable","sources":["company"]}}`},
		{"invalid candidate on server", 422, `{"error":{"code":"invalid_config","message":"unknown credential"}}`},
		{"busy", 429, `{"error":{"code":"busy","message":"dry-run already running"}}`},
		{"bad success JSON", 200, "not JSON"},
		{"incomplete success", 200, `{"version":1}`},
		{"future report version", 200, `{"version":2,"baseRevision":"a","candidateRevision":"b","filters":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opts := options(&stdout, &stderr)
			opts.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(tc.status, tc.body), nil })}
			code := cli.Run(context.Background(), []string{"dry-run", "--config", path, "--against", "https://discovery.example", "--output", "json"}, opts)
			if code != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
		})
	}
}

func TestDryRunRequestCarriesCallerCancellationAndDeadline(t *testing.T) {
	path := writeConfig(t, configYAML)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	wantDeadline, _ := ctx.Deadline()
	calls := 0
	var stdout, stderr bytes.Buffer
	opts := options(&stdout, &stderr)
	opts.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		gotDeadline, ok := r.Context().Deadline()
		if !ok || !gotDeadline.Equal(wantDeadline) {
			t.Fatal("request lost caller deadline")
		}
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	if code := cli.Run(ctx, []string{"dry-run", "--config", path, "--against", "https://discovery.example"}, opts); code != 1 || calls != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("cancel exit=%d calls=%d stderr=%s", code, calls, &stderr)
	}
}

func TestServePassesFrozenInputsAndOnlyDocumentedEnvToRuntime(t *testing.T) {
	path := writeConfig(t, configYAML)
	envConfig := writeConfig(t, "version: 99\n")
	var stdout, stderr bytes.Buffer
	opts := options(&stdout, &stderr)
	opts.LookupEnv = env(map[string]string{"FRD_CONFIG": envConfig, "FRD_CREDENTIALS_FILE": "env-credentials.yaml", "FRD_LISTEN": ":9090", "FRD_SCAN_INTERVAL": "30s", "FRD_SCAN_TIMEOUT": "15s", "FRD_GITHUB_TOKEN": "secret"})
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "caller")
	calls := 0
	opts.Serve = func(gotCtx context.Context, got cli.ServeOptions) error {
		calls++
		if gotCtx.Value(contextKey{}) != "caller" {
			t.Fatal("serve lost caller context")
		}
		if got.CredentialsFile != "flag-credentials.yaml" {
			t.Fatalf("credentials flag precedence: %q", got.CredentialsFile)
		}
		if got.Config.Server().Listen != ":8080" || time.Duration(got.Config.Scan().Interval) != 10*time.Minute {
			t.Fatal("declared configuration overwritten before service captures it")
		}
		want := map[string]string{"FRD_LISTEN": ":9090", "FRD_SCAN_INTERVAL": "30s", "FRD_SCAN_TIMEOUT": "15s"}
		if !reflect.DeepEqual(got.Env, want) {
			t.Fatalf("runtime env = %#v", got.Env)
		}
		return nil
	}
	code := cli.Run(ctx, []string{"serve", "--config", path, "--credentials-file", "flag-credentials.yaml"}, opts)
	if code != 0 || calls != 1 || stderr.Len() != 0 {
		t.Fatalf("serve exit=%d calls=%d stderr=%s", code, calls, &stderr)
	}
	opts.LookupEnv = env(map[string]string{"FRD_CONFIG": path, "FRD_CREDENTIALS_FILE": "env-credentials.yaml"})
	opts.Serve = func(_ context.Context, got cli.ServeOptions) error {
		if got.CredentialsFile != "env-credentials.yaml" {
			t.Fatal("credentials env ignored")
		}
		return errors.New("startup failed")
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"serve"}, opts)
	if code != 1 || stderr.Len() == 0 {
		t.Fatalf("serve failure exit=%d stderr=%s", code, &stderr)
	}
}
