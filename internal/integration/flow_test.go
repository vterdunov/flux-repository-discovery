package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/cli"
	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/httpapi"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func inMemoryHTTP(handler http.Handler) *http.Client {
	return &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		response := recorder.Result()
		response.Request = r
		return response, nil
	})}
}

type githubRepository struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	Archived bool     `json:"archived"`
	Fork     bool     `json:"fork"`
	Topics   []string `json:"topics"`
}

func fixtureRepository(id int, renamed bool) githubRepository {
	name := fmt.Sprintf("repo%03d", id)
	if id == 1 && renamed {
		name = "renamed"
	}
	r := githubRepository{ID: id, Name: name, FullName: "acme/" + name, Topics: []string{"gitops"}}
	r.Owner.Login = "acme"
	return r
}
func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}
func get(handler http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
	return w
}

// This is a component integration test, not a claim that a live Flux Operator
// has been tested. Both HTTP boundaries use deterministic in-memory transports.
func TestCLIThroughHTTPAndGitHubPreservesSafetyAcrossWholeWorkflow(t *testing.T) {
	declared := `version: 1
sources: {company: {owners: [acme], credential: token}}
filters: {apps: {sources: [company], include: [{topics: {all: [gitops]}}]}}
`
	cfg, err := config.Decode([]byte(declared))
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := github.LoadCredentials([]byte("credentials: {token: {token: {valueEnv: TEST_PAT}}}"), github.CredentialOptions{LookupEnv: func(name string) (string, bool) { return "integration-secret", name == "TEST_PAT" }})
	if err != nil {
		t.Fatal(err)
	}
	phase := "published"
	pageCalls := 0
	githubHTTP := inMemoryHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer integration-secret" {
			t.Fatalf("GitHub boundary identity invalid: %s", r.URL.Redacted())
		}
		switch r.URL.Path {
		case "/users/acme":
			writeJSON(t, w, struct {
				Login string `json:"login"`
				Type  string `json:"type"`
			}{"acme", "Organization"})
		case "/orgs/acme/repos":
			pageCalls++
			if phase == "empty" {
				writeJSON(t, w, []githubRepository{})
				return
			}
			page2 := r.URL.Query().Get("page") == "2"
			if page2 && phase == "failure" {
				http.Error(w, "upstream failed", 500)
				return
			}
			first, last := 1, 100
			if page2 {
				first, last = 101, 120
			} else {
				w.Header().Set("Link", `<https://api.github.com/orgs/acme/repos?type=all&per_page=100&page=2>; rel="next"`)
			}
			repositories := make([]githubRepository, 0, last-first+1)
			for id := first; id <= last; id++ {
				repositories = append(repositories, fixtureRepository(id, phase != "published"))
			}
			writeJSON(t, w, repositories)
		default:
			t.Fatalf("unexpected GitHub endpoint %s", r.URL.Redacted())
		}
	}))
	client, err := github.NewClient(credentials, github.Options{HTTPClient: githubHTTP, Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.Prepare(cfg, nil, credentials.Names())
	if err != nil {
		t.Fatal(err)
	}
	s, err := service.New(runtime, client, service.Options{})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(s, httpapi.Options{})
	if w := get(handler, "/inputs/apps"); w.Code != 503 {
		t.Fatalf("before scan HTTP=%d %s", w.Code, w.Body)
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if pageCalls != 2 {
		t.Fatalf("initial pagination calls=%d", pageCalls)
	}
	w := get(handler, "/inputs/apps")
	var initial struct {
		Inputs []discovery.Input `json:"inputs"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || len(initial.Inputs) != 120 || initial.Inputs[0].Name != "repo001" {
		t.Fatalf("initial complete output: HTTP=%d count=%d", w.Code, len(initial.Inputs))
	}
	statusBefore := s.Status()
	phase = "preview"
	pageCalls = 0
	candidate := `version: 1
sources: {company: {owners: [acme], credential: token}}
filters: {apps: {sources: [company], include: [{repositories: [acme/renamed, acme/repo002, acme/repo003, acme/missing]}]}}
`
	path := filepath.Join(t.TempDir(), "candidate.yaml")
	if err := os.WriteFile(path, []byte(candidate), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	operatorHTTP := inMemoryHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host != "discovery.local" || r.Header.Get("Authorization") != "" {
			t.Fatal("operator request leaked GitHub credentials or bypassed service")
		}
		handler.ServeHTTP(w, r)
	}))
	opts := cli.Options{Stdout: &stdout, Stderr: &stderr, LookupEnv: func(string) (string, bool) { return "", false }, HTTPClient: operatorHTTP}
	args := []string{"dry-run", "--config", path, "--against", "http://discovery.local", "--output", "json", "--detailed-exitcode"}
	if code := cli.Run(context.Background(), args, opts); code != 3 || stderr.Len() != 0 {
		t.Fatalf("preview CLI code=%d stderr=%s", code, &stderr)
	}
	var report service.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	apps := report.Filters["apps"]
	if apps.BeforeCount != 120 || apps.AfterCount != 3 || len(apps.Removed) != 117 || len(apps.Inputs) != 3 || len(apps.Changed) != 0 || !reflect.DeepEqual(apps.UnmatchedRepositories, []string{"acme/missing"}) {
		t.Fatalf("large removal diff incomplete: %#v", apps)
	}
	if pageCalls != 2 {
		t.Fatalf("current and candidate did not reuse fresh catalog: page calls=%d", pageCalls)
	}
	drift := report.Drift["apps"]
	if !drift.Available || len(drift.Diff.Changed) != 1 || drift.Diff.Changed[0].Before.ID != "1" || drift.Diff.Changed[0].After.Name != "renamed" {
		t.Fatalf("rename drift missing: %#v", drift)
	}
	if !reflect.DeepEqual(s.Status(), statusBefore) {
		t.Fatal("preview mutated published status")
	}
	w = get(handler, "/inputs/apps")
	if !strings.Contains(w.Body.String(), `"name":"repo001"`) {
		t.Fatal("preview mutated published inputs")
	}
	if strings.Contains(stdout.String(), "integration-secret") {
		t.Fatal("secret leaked in report")
	}
	phase = "failure"
	stdout.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), args, opts); code != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("failed dry-run produced success: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	if !reflect.DeepEqual(s.Status(), statusBefore) {
		t.Fatal("failed preview mutated published status")
	}
	if err := s.Scan(context.Background()); err == nil {
		t.Fatal("failed second page did not fail scan")
	}
	w = get(handler, "/inputs/apps")
	if w.Code != 503 || strings.Contains(w.Body.String(), `"inputs"`) {
		t.Fatalf("partial page or stale output published: %d %s", w.Code, w.Body)
	}
	phase = "empty"
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	w = get(handler, "/inputs/apps")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"inputs":[]}` {
		t.Fatalf("successful empty recovery: %d %s", w.Code, w.Body)
	}
}
