//go:build (integration || e2e) && (darwin || linux)

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
		if strings.HasPrefix(name, "FRD_") || strings.HasSuffix(name, "_PROXY") || name == "PROXY" || name == "GOOS" || name == "GOARCH" || name == "GH_TOKEN" || name == "GITHUB_TOKEN" {
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
	secrets := append(environmentSecrets(os.Environ()), environmentSecrets(env)...)
	stdout, stderr := &boundedCapture{secrets: secrets}, &boundedCapture{secrets: secrets}
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
	redactor := boundedCapture{secrets: environmentSecrets(os.Environ())}
	safeBody := redactor.redact(string(body))
	if err != nil || len(body) > captureLimit || response.Header.Get("Cache-Control") != "no-store" || !strings.HasPrefix(response.Header.Get("Content-Type"), "application/json") || safeBody != string(body) {
		t.Fatalf("GET %s invalid bounded JSON response: HTTP=%d body=%q error=%v", address, response.StatusCode, safeBody, err)
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
	secrets   []string
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
		if c.redact(string(c.data)) != string(c.data) {
			c.issue = errors.New("child output leaked credential")
		}
		if c.jsonLines && c.issue == nil {
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
	return c.redact(string(c.data))
}

func environmentSecrets(env []string) []string {
	secrets := []string{fakeToken}
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		if strings.HasSuffix(name, "_TOKEN") && value != "" {
			secrets = append(secrets, value)
		}
	}
	return secrets
}

func (c *boundedCapture) redact(value string) string {
	value = strings.ReplaceAll(value, fakeToken, "[redacted]")
	for _, secret := range c.secrets {
		value = strings.ReplaceAll(value, secret, "[redacted]")
	}
	return value
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

type binaryServer struct {
	command      *exec.Cmd
	done         <-chan struct{}
	waitErr      *error // Only read after done is closed.
	stdout, logs *boundedCapture
	endpoint     string
}

// startServer owns the child process, dynamic listener and redacted diagnostics.
func startServer(t *testing.T, ctx context.Context, root string, env []string, binary, configuration, credentials string) *binaryServer {
	t.Helper()
	server := exec.CommandContext(ctx, binary, "serve", "--config", configuration, "--credentials-file", credentials)
	server.Dir, server.Env, server.WaitDelay = root, env, 2*time.Second
	serverOut := &boundedCapture{secrets: environmentSecrets(env)}
	serverLogs := &boundedCapture{secrets: environmentSecrets(env), jsonLines: true, changed: make(chan struct{}, 1)}
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
		if serverLogs.problem() != nil || serverOut.problem() != nil || serverOut.text() != "" {
			t.Errorf("server output contract: stderr=%v stdout=%v", serverLogs.problem(), serverOut.problem())
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
	return &binaryServer{command: server, done: done, waitErr: &waitErr, stdout: serverOut, logs: serverLogs, endpoint: "http://" + address}
}
