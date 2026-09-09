//go:build integration && (darwin || linux)

package cli_test

import (
	"context"
	"encoding/json/jsontext"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
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
	server := startServer(t, serverContext, root, env, binary, configuration, credentials)
	startup, cancelStartup := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancelStartup()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	var before statusDocument
	for {
		readyCode, _, readyBody := get(t, startup, client, server.endpoint+"/readyz")
		statusCode, _, statusBody := get(t, startup, client, server.endpoint+"/api/v1/status")
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
		case <-server.done:
			t.Fatalf("server exited before first generation: %v", *server.waitErr)
		case <-startup.Done():
			t.Fatal("readiness and first generation did not complete before startup deadline")
		}
	}
	if !isRevision(before.Revision) || before.ScannedAt.IsZero() || before.Sources == nil || len(before.Sources) != 0 || before.Filters == nil || len(before.Filters) != 0 {
		t.Fatalf("initial status: %#v", before)
	}
	code, _, body := get(t, t.Context(), client, server.endpoint+"/healthz")
	var health struct {
		Status string `json:"status"`
	}
	decode(t, body, &health)
	if code != http.StatusOK || health.Status != "ok" {
		t.Fatalf("liveness HTTP %d: %s", code, body)
	}
	code, _, body = get(t, t.Context(), client, server.endpoint+"/inputs/missing")
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

	stdout, stderr = runCommand(t, root, env, 10*time.Second, 3, binary, "dry-run", "--config", candidate, "--against", server.endpoint, "--output", "json", "--detailed-exitcode")
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
	code, _, body = get(t, t.Context(), client, server.endpoint+"/api/v1/status")
	var after statusDocument
	decode(t, body, &after)
	if code != http.StatusOK || !reflect.DeepEqual(before, after) {
		t.Fatalf("preview changed publication: before=%#v after=%#v HTTP=%d", before, after, code)
	}
	if err := server.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.done:
		if *server.waitErr != nil {
			t.Fatalf("SIGTERM exit: %v", *server.waitErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop within graceful shutdown deadline")
	}
	_, stopped, _ := server.logs.state()
	logErr := server.logs.problem()
	if logErr != nil || !stopped || server.stdout.text() != "" || server.stdout.problem() != nil {
		t.Fatalf("shutdown log/stdout contract: stopped=%t logs=%v stdout=%q capture=%v", stopped, logErr, server.stdout.text(), server.stdout.problem())
	}
}

func TestCapturedCredentialsAreRedacted(t *testing.T) {
	const secret = "integration-live-token-not-a-real-credential"
	for _, jsonLines := range []bool{false, true} {
		capture := &boundedCapture{secrets: []string{secret}, jsonLines: jsonLines}
		// Deliberately malformed JSON also checks that a parse error cannot
		// replace the safe credential-leak error with unredacted diagnostics.
		for _, chunk := range []string{`{"msg":"`, secret[:12], secret[12:] + "\n"} {
			if _, err := capture.Write([]byte(chunk)); err != nil {
				t.Fatal(err)
			}
		}
		if capture.problem() == nil || strings.Contains(capture.problem().Error(), secret) || strings.Contains(capture.text(), secret) || !strings.Contains(capture.text(), "[redacted]") {
			t.Fatalf("unsafe capture with jsonLines=%t", jsonLines)
		}
	}
}
