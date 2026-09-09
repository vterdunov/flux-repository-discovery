//go:build integration && (darwin || linux)

package cli_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	captureLimit = 64 * 1024
	fakeToken    = "cli-integration-not-a-real-github-token"
	buildVersion = "cli-integration-test"
)

// This test deliberately imports no application packages. Both the server and
// operator CLI execute the current source as real child processes.
func TestBinaryHTTPDryRunAndGracefulShutdown(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	folder := t.TempDir()
	binary := filepath.Join(folder, "flux-repository-discovery")
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	env := isolatedEnvironment()
	runCommand(t, root, env, 45*time.Second, 0, goBinary, "build", "-race", "-ldflags=-X main.version="+buildVersion, "-o", binary, "./cmd/flux-repository-discovery")
	stdout, stderr := runCommand(t, root, env, 5*time.Second, 0, binary, "version")
	if stdout != buildVersion+"\n" || stderr != "" {
		t.Fatalf("fresh binary version: stdout=%q stderr=%q", stdout, stderr)
	}
	stdout, stderr = runCommand(t, root, env, 5*time.Second, 0, binary, "validate", "--config", "examples/config.yaml")
	if stdout != "Configuration is valid.\n" || stderr != "" {
		t.Fatalf("example validation: stdout=%q stderr=%q", stdout, stderr)
	}
	writeFixture := func(name, content string) string {
		t.Helper()
		path := filepath.Join(folder, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	configuration := writeFixture("config.yaml", "version: 1\nserver: {listen: '127.0.0.1:0'}\nsources: {}\nfilters: {}\n")
	candidate := writeFixture("candidate.yaml", "version: 1\nserver: {listen: '127.0.0.1:0'}\nscan: {interval: 5m}\nsources: {}\nfilters: {}\n")
	credentials := writeFixture("credentials.yaml", "credentials: {smoke: {token: {valueEnv: FRD_CLI_TEST_TOKEN}}}\n")
	env = append(env, "FRD_CLI_TEST_TOKEN="+fakeToken)

	serverContext, cancelServer := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancelServer()
	server := exec.CommandContext(serverContext, binary, "serve", "--config", configuration, "--credentials-file", credentials)
	server.Dir, server.Env, server.WaitDelay = root, env, 2*time.Second
	serverOut := &boundedCapture{}
	serverLogs := &boundedCapture{jsonLines: true, changed: make(chan struct{}, 1)}
	server.Stdout, server.Stderr = serverOut, serverLogs
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() { waitErr = server.Wait(); close(done) }()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			_ = server.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = server.Process.Kill()
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Error("server was not reaped after Kill")
				}
			}
		}
		if t.Failed() {
			t.Logf("server stdout:\n%s\nserver stderr:\n%s", serverOut.text(), serverLogs.text())
		}
	})

	startup, cancelStartup := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancelStartup()
	var address string
	for address == "" {
		var logErr error
		address, _, logErr = serverLogs.state()
		if logErr != nil {
			t.Fatal(logErr)
		}
		if address != "" {
			break
		}
		select {
		case <-serverLogs.changed:
		case <-done:
			t.Fatalf("server exited before announcing listener: %v", waitErr)
		case <-startup.Done():
			t.Fatal("server did not announce listener before startup deadline")
		}
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port == "" || port == "0" {
		t.Fatalf("server did not use dynamic localhost listener: %q (%v)", address, err)
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	endpoint := "http://" + address
	var before statusDocument
	for {
		readyCode, _, readyBody := get(t, startup, client, endpoint+"/readyz")
		statusCode, _, statusBody := get(t, startup, client, endpoint+"/api/v1/status")
		if statusCode != http.StatusOK {
			t.Fatalf("status HTTP %d: %s", statusCode, statusBody)
		}
		decode(t, statusBody, &before)
		if readyCode == http.StatusOK && before.Generation > 0 {
			var ready struct {
				Status string `json:"status"`
			}
			decode(t, readyBody, &ready)
			if ready.Status != "ready" {
				t.Fatalf("readiness body: %s", readyBody)
			}
			break
		}
		if readyCode != http.StatusOK && readyCode != http.StatusServiceUnavailable {
			t.Fatalf("readiness HTTP %d: %s", readyCode, readyBody)
		}
		select {
		case <-time.After(10 * time.Millisecond):
		case <-done:
			t.Fatalf("server exited before first generation: %v", waitErr)
		case <-startup.Done():
			t.Fatal("readiness and first generation did not complete before startup deadline")
		}
	}
	if !isRevision(before.Revision) || before.ScannedAt.IsZero() || before.Sources == nil || len(before.Sources) != 0 || before.Filters == nil || len(before.Filters) != 0 {
		t.Fatalf("initial status: %#v", before)
	}
	code, _, body := get(t, t.Context(), client, endpoint+"/healthz")
	var health struct {
		Status string `json:"status"`
	}
	decode(t, body, &health)
	if code != http.StatusOK || health.Status != "ok" {
		t.Fatalf("liveness HTTP %d: %s", code, body)
	}
	code, _, body = get(t, t.Context(), client, endpoint+"/inputs/missing")
	var missing struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
		Inputs jsontext.Value `json:"inputs"`
	}
	decode(t, body, &missing)
	if code != http.StatusNotFound || missing.Error.Code != "not_found" || len(missing.Inputs) != 0 {
		t.Fatalf("missing filter HTTP %d: %s", code, body)
	}

	stdout, stderr = runCommand(t, root, env, 10*time.Second, 3, binary, "dry-run", "--config", candidate, "--against", endpoint, "--output", "json", "--detailed-exitcode")
	if stderr != "" {
		t.Fatalf("successful preview wrote stderr: %q", stderr)
	}
	var report reportDocument
	decode(t, []byte(stdout), &report)
	if report.Version != 1 || !report.HasChanges || report.BaseRevision != before.Revision || !isRevision(report.CandidateRevision) || report.CandidateRevision == before.Revision || report.BaseGeneration != before.Generation || report.ObservedAt.IsZero() {
		t.Fatalf("preview metadata: %#v", report)
	}
	for _, changes := range [][]changeDocument{report.ConfigChanges, report.EffectiveChanges} {
		if len(changes) != 1 || changes[0].Operation != "replace" || changes[0].Path != "scan.interval" || string(changes[0].Before) != `"10m0s"` || string(changes[0].After) != `"5m0s"` {
			t.Fatalf("preview must contain complete declared/effective interval diff: %#v", changes)
		}
	}
	if report.Filters == nil || len(report.Filters) != 0 || report.Drift == nil || len(report.Drift) != 0 || report.Overrides == nil || len(report.Overrides) != 0 {
		t.Fatalf("empty catalog preview contains unexpected or missing maps: %#v", report)
	}
	code, _, body = get(t, t.Context(), client, endpoint+"/api/v1/status")
	var after statusDocument
	decode(t, body, &after)
	if code != http.StatusOK || !reflect.DeepEqual(before, after) {
		t.Fatalf("preview changed publication: before=%#v after=%#v HTTP=%d", before, after, code)
	}
	if err := server.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		if waitErr != nil {
			t.Fatalf("SIGTERM exit: %v", waitErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop within graceful shutdown deadline")
	}
	_, stopped, _ := serverLogs.state()
	logErr := serverLogs.problem()
	if logErr != nil || !stopped || serverOut.text() != "" || serverOut.problem() != nil {
		t.Fatalf("shutdown log/stdout contract: stopped=%t logs=%v stdout=%q capture=%v", stopped, logErr, serverOut.text(), serverOut.problem())
	}
}

type statusDocument struct {
	Revision   string                    `json:"revision"`
	Generation uint64                    `json:"generation"`
	ScannedAt  time.Time                 `json:"scannedAt"`
	Sources    map[string]jsontext.Value `json:"sources"`
	Filters    map[string]jsontext.Value `json:"filters"`
}

type changeDocument struct {
	Operation string         `json:"operation"`
	Path      string         `json:"path"`
	Before    jsontext.Value `json:"before"`
	After     jsontext.Value `json:"after"`
}

type reportDocument struct {
	Version           int                       `json:"version"`
	BaseRevision      string                    `json:"baseRevision"`
	CandidateRevision string                    `json:"candidateRevision"`
	BaseGeneration    uint64                    `json:"baseGeneration"`
	ObservedAt        time.Time                 `json:"observedAt"`
	HasChanges        bool                      `json:"hasChanges"`
	ConfigChanges     []changeDocument          `json:"configChanges"`
	EffectiveChanges  []changeDocument          `json:"effectiveChanges"`
	Overrides         map[string]string         `json:"overrides"`
	Filters           map[string]jsontext.Value `json:"filters"`
	Drift             map[string]jsontext.Value `json:"drift"`
}

func isolatedEnvironment() []string {
	var result []string
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		name = strings.ToUpper(name)
		if strings.HasPrefix(name, "FRD_") || strings.HasSuffix(name, "_PROXY") || name == "PROXY" || name == "GOOS" || name == "GOARCH" {
			continue
		}
		result = append(result, value)
	}
	return append(result, "GOOS="+runtime.GOOS, "GOARCH="+runtime.GOARCH)
}

func runCommand(t *testing.T, dir string, env []string, timeout time.Duration, exitCode int, executable string, args ...string) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Dir, command.Env, command.WaitDelay = dir, env, 2*time.Second
	stdout, stderr := &boundedCapture{}, &boundedCapture{}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	var exitErr *exec.ExitError
	unexpectedErr := err != nil && (exitCode == 0 || !errors.As(err, &exitErr))
	if ctx.Err() != nil || command.ProcessState == nil || command.ProcessState.ExitCode() != exitCode || unexpectedErr || stdout.problem() != nil || stderr.problem() != nil {
		t.Fatalf("%s %v failed: error=%v context=%v stdout=%q stderr=%q capture=%v/%v", filepath.Base(executable), args, err, ctx.Err(), stdout.text(), stderr.text(), stdout.problem(), stderr.problem())
	}
	return stdout.text(), stderr.text()
}

func get(t *testing.T, ctx context.Context, client *http.Client, address string) (int, http.Header, []byte) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", address, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, captureLimit+1))
	if err != nil || len(body) > captureLimit || response.Header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") || bytes.Contains(body, []byte(fakeToken)) {
		t.Fatalf("GET %s invalid bounded JSON response: HTTP=%d headers=%v body=%q error=%v", address, response.StatusCode, response.Header, bytes.ReplaceAll(body, []byte(fakeToken), []byte("[redacted]")), err)
	}
	return response.StatusCode, response.Header, body
}

func decode(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("invalid JSON response %q: %v", data, err)
	}
}

func isRevision(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == 32
}

// os/exec owns the continuous pipe-draining goroutines. A bounded writer avoids
// coupling startup waits to descriptor readiness or blocking on a full pipe.
type boundedCapture struct {
	mu        sync.Mutex
	data      []byte
	pending   []byte
	issue     error
	jsonLines bool
	address   string
	stopped   bool
	changed   chan struct{}
}

func (c *boundedCapture) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.issue != nil {
		return len(data), nil
	}
	if len(c.data)+len(data) > captureLimit {
		c.issue = errors.New("child output exceeded capture limit")
	} else {
		c.data = append(c.data, data...)
		if bytes.Contains(c.data, []byte(fakeToken)) {
			c.issue = errors.New("child output leaked fake credential")
		}
		if c.jsonLines {
			c.pending = append(c.pending, data...)
			for {
				line, rest, ok := bytes.Cut(c.pending, []byte{'\n'})
				if !ok {
					break
				}
				c.pending = rest
				var event struct {
					Message string `json:"msg"`
					Address string `json:"address"`
				}
				if err := json.Unmarshal(line, &event); err != nil {
					c.issue = fmt.Errorf("invalid JSON log: %w", err)
					break
				}
				if event.Message == "discovery server started" {
					c.address = event.Address
				}
				if event.Message == "discovery server stopped" {
					c.stopped = true
				}
			}
		}
	}
	if c.changed != nil {
		select {
		case c.changed <- struct{}{}:
		default:
		}
	}
	return len(data), nil
}

func (c *boundedCapture) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.ReplaceAll(string(c.data), fakeToken, "[redacted]")
}

func (c *boundedCapture) problem() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.issue == nil && c.jsonLines && len(c.pending) != 0 {
		return errors.New("child closed stderr with an incomplete JSON log")
	}
	return c.issue
}

func (c *boundedCapture) state() (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.address, c.stopped, c.issue
}
