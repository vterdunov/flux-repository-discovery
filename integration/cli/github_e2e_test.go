//go:build e2e && (darwin || linux)

package cli_test

import (
	"context"
	"encoding/json/v2"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const fixtureOwner = "vterdunov"

// This is a read-only, live GitHub test of the real binary. A missing token is a
// failure when the e2e build tag is selected, never a silently skipped check.
func TestGitHubDiscovery(t *testing.T) {
	token := strings.TrimSpace(os.Getenv("FRD_E2E_GITHUB_TOKEN"))
	if token == "" {
		t.Fatal("FRD_E2E_GITHUB_TOKEN is required; see integration/cli/testdata/github/README.md")
	}
	fixtures := verifyGitHubFixtures(t, token)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "flux-repository-discovery")
	env := isolatedEnvironment()
	runCommand(t, root, env, 90*time.Second, 0, "go", "build", "-race", "-o", binary, "./cmd/flux-repository-discovery")
	env = append(env, "FRD_E2E_GITHUB_TOKEN="+token)
	configuration := filepath.Join(root, "integration/cli/testdata/github/config.yaml")
	credentials := filepath.Join(root, "integration/cli/testdata/github/credentials.yaml")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)
	server := startServer(t, ctx, root, env, binary, configuration, credentials)
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}

	// Readiness only confirms the scan loop. Wait for a published generation,
	// then fail immediately if its source failed, rather than retrying assertions.
	scanCtx, cancelScan := context.WithTimeout(ctx, 130*time.Second)
	defer cancelScan()
	var before statusDocument
	for {
		code, _, body := get(t, scanCtx, client, server.endpoint+"/api/v1/status")
		if code != http.StatusOK {
			t.Fatalf("status HTTP %d: %s", code, body)
		}
		decode(t, body, &before)
		if before.Generation > 0 {
			break
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-server.done:
			t.Fatalf("server exited during scan: %v", *server.waitErr)
		case <-scanCtx.Done():
			t.Fatal("first GitHub scan did not finish within 130 seconds")
		}
	}
	if len(before.Sources) != 2 || !isRevision(before.Revision) || before.ScannedAt.IsZero() {
		t.Fatalf("invalid publication metadata: %+v", before)
	}
	for name, raw := range before.Sources {
		var source struct {
			Ready bool `json:"ready"`
		}
		decode(t, raw, &source)
		if !source.Ready {
			t.Fatalf("GitHub source %s failed: %s", name, raw)
		}
	}
	selected := map[string][]string{
		"catalog":       {"api", "worker", "excluded", "unmatched"},
		"production":    {"api"},
		"backends":      {"api", "worker"},
		"exact":         {"api", "worker"},
		"regex":         {"api", "worker"},
		"union":         {"api", "worker"},
		"exclude-exact": {"api", "worker"},
		"empty":         {},
		"deduplicated":  {"api"},
	}
	if len(before.Filters) != len(selected) {
		t.Fatalf("unexpected filters: %v", before.Filters)
	}
	for _, name := range sortedKeys(selected) {
		t.Run(name, func(t *testing.T) {
			assertInputs(t, ctx, client, server.endpoint, name, fixtureInputs(fixtures, selected[name]...))
			var filter struct {
				Ready bool `json:"ready"`
				Count int  `json:"count"`
			}
			decode(t, before.Filters[name], &filter)
			if !filter.Ready || filter.Count != len(selected[name]) {
				t.Fatalf("filter status does not match inputs: %+v", filter)
			}
		})
	}
	if t.Failed() {
		return
	}

	t.Run("dry-run", func(t *testing.T) {
		data, err := os.ReadFile(configuration)
		if err != nil {
			t.Fatal(err)
		}
		candidate := filepath.Join(t.TempDir(), "candidate.yaml")
		data = []byte(strings.Replace(string(data), "all: [frd-e2e, gitops, production]", "all: [frd-e2e, gitops, staging]", 1))
		if err := os.WriteFile(candidate, data, 0o600); err != nil {
			t.Fatal(err)
		}
		// The CLI receives no discovery token. Only the server scans GitHub.
		stdout, stderr := runCommand(t, root, isolatedEnvironment(), 130*time.Second, 3, binary, "dry-run", "--config", candidate, "--against", server.endpoint, "--output", "json", "--detailed-exitcode")
		if stderr != "" {
			t.Fatalf("dry-run stderr: %s", stderr)
		}
		var report reportDocument
		decode(t, []byte(stdout), &report)
		if !report.HasChanges || report.BaseRevision != before.Revision || report.BaseGeneration != before.Generation || report.CandidateRevision == before.Revision || !isRevision(report.CandidateRevision) {
			t.Fatalf("invalid dry-run metadata: %+v", report)
		}
		var diff struct {
			Kind    string        `json:"kind"`
			Added   []githubInput `json:"added"`
			Removed []githubInput `json:"removed"`
			Inputs  []githubInput `json:"inputs"`
		}
		decode(t, report.Filters["production"], &diff)
		if diff.Kind != "modified" || !reflect.DeepEqual(diff.Added, fixtureInputs(fixtures, "worker")) || !reflect.DeepEqual(diff.Removed, fixtureInputs(fixtures, "api")) || !reflect.DeepEqual(diff.Inputs, fixtureInputs(fixtures, "worker")) {
			t.Fatalf("unexpected production preview: %+v", diff)
		}
		code, _, body := get(t, ctx, client, server.endpoint+"/api/v1/status")
		var after statusDocument
		decode(t, body, &after)
		if code != http.StatusOK || !reflect.DeepEqual(before, after) {
			t.Fatalf("dry-run changed publication: before=%+v after=%+v", before, after)
		}
		assertInputs(t, ctx, client, server.endpoint, "production", fixtureInputs(fixtures, "api"))
	})
	if err := server.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case <-server.done:
		if *server.waitErr != nil {
			t.Fatalf("server shutdown: %v", *server.waitErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("server did not shut down within 10 seconds")
	}
}

type githubFixture struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Private  bool     `json:"private"`
	Archived bool     `json:"archived"`
	Fork     bool     `json:"fork"`
	Topics   []string `json:"topics"`
}

type githubInput struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Owner    string `json:"owner"`
	Name     string `json:"name"`
}

// Pin identity AND negative fixture metadata. Otherwise an inaccessible excluded
// or archived repository could make exclusion tests pass without exercising it.
func verifyGitHubFixtures(t *testing.T, token string) map[string]githubFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/github/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []githubFixture
	decode(t, data, &fixtures)
	client := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(client.CloseIdleConnections)
	result := make(map[string]githubFixture, len(fixtures))
	for _, want := range fixtures {
		address := "https://api.github.com/repos/" + fixtureOwner + "/" + want.Name
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("fixture %s: GitHub request failed", want.Name)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, captureLimit+1))
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK || readErr != nil || len(body) > captureLimit {
			t.Fatalf("fixture %s: HTTP %d or unreadable metadata; verify token access and fixture existence", want.Name, response.StatusCode)
		}
		if strings.Contains(string(body), token) {
			t.Fatal("GitHub response contained the credential")
		}
		var got githubFixture
		decode(t, body, &got)
		slices.Sort(got.Topics)
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("fixture metadata changed: want=%+v got=%+v", want, got)
		}
		result[strings.TrimPrefix(want.Name, "frd-e2e-")] = want
	}
	return result
}

func fixtureInputs(fixtures map[string]githubFixture, names ...string) []githubInput {
	result := make([]githubInput, 0, len(names))
	for _, name := range names {
		fixture := fixtures[name]
		result = append(result, githubInput{ID: strconv.FormatInt(fixture.ID, 10), FullName: fixtureOwner + "/" + fixture.Name, Owner: fixtureOwner, Name: fixture.Name})
	}
	slices.SortFunc(result, func(a, b githubInput) int {
		x, _ := strconv.ParseInt(a.ID, 10, 64)
		y, _ := strconv.ParseInt(b.ID, 10, 64)
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
		return 0
	})
	return result
}

func assertInputs(t *testing.T, ctx context.Context, client *http.Client, endpoint, filter string, want []githubInput) {
	t.Helper()
	code, _, body := get(t, ctx, client, endpoint+"/inputs/"+filter)
	var envelope struct {
		Inputs []githubInput `json:"inputs"`
	}
	if err := json.Unmarshal(body, &envelope, json.RejectUnknownMembers(true)); err != nil {
		t.Fatalf("%s invalid inputs (HTTP %d): %s", filter, code, body)
	}
	if code != http.StatusOK || !reflect.DeepEqual(envelope.Inputs, want) || (len(want) == 0 && string(body) != `{"inputs":[]}`) {
		t.Fatalf("%s: HTTP %d want=%+v got=%s", filter, code, want, body)
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
