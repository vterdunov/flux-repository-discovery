package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/httpapi"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

type scannerFunc func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result

func (f scannerFunc) Scan(ctx context.Context, s map[config.SourceName]config.Source) map[config.SourceName]github.Result {
	return f(ctx, s)
}
func apiConfig() config.Document {
	return config.Document{Version: 1, Server: config.Server{Listen: ":8080"}, Scan: config.Scan{Interval: config.Duration(10 * time.Minute), Timeout: config.Duration(2 * time.Minute)}, Sources: map[config.SourceName]config.Source{"company": {Owners: []string{"acme"}, Credential: "token"}}, Filters: map[config.FilterName]config.FilterSpec{"apps": {Sources: []config.SourceName{"company"}, Include: []config.Rule{{NameRegex: ".*"}}}}}
}
func newService(t *testing.T, scan service.Scanner, environments ...map[string]string) *service.Service {
	t.Helper()
	var environment map[string]string
	if len(environments) > 0 {
		environment = environments[0]
	}
	parsed, err := config.Parse(apiConfig())
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := config.Prepare(parsed, environment, []config.CredentialName{"token"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := service.New(runtime, scan, service.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func request(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
func requireError(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if w.Code != status {
		t.Fatalf("HTTP %d, want %d: %s", w.Code, status, w.Body)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("non-JSON error: %v", w.Header())
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if _, ok := value["inputs"]; ok {
		t.Fatal("error contains inputs")
	}
	var errBody service.Error
	if err := json.Unmarshal(value["error"], &errBody); err != nil {
		t.Fatal(err)
	}
	if string(errBody.Code) != code {
		t.Fatalf("code=%q, want %q: %s", errBody.Code, code, w.Body)
	}
}

func TestFluxEndpointSuccessEmptyFailureAndRecovery(t *testing.T) {
	current := github.Result{Repositories: []discovery.Repository{{ID: 2, Owner: "Acme", Name: "api"}, {ID: 1, Owner: "Acme", Name: "worker"}}}
	s := newService(t, scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		return map[config.SourceName]github.Result{"company": current}
	}))
	handler := httpapi.New(s, httpapi.Options{})
	w := request(handler, http.MethodGet, "/inputs/apps", "")
	requireError(t, w, 503, "not_ready")
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("initial error may be cached")
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	w = request(handler, http.MethodGet, "/inputs/apps", "")
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body)
	}
	want := `{"inputs":[{"id":"1","fullName":"Acme/worker","owner":"Acme","name":"worker"},{"id":"2","fullName":"Acme/api","owner":"Acme","name":"api"}]}`
	if strings.TrimSpace(w.Body.String()) != want {
		t.Fatalf("Flux wire contract: %s", w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("success may be cached")
	}
	current = github.Result{Repositories: []discovery.Repository{{ID: 3, Owner: "Acme", Name: "partial"}}, Err: errors.New("credential-secret")}
	_ = s.Scan(context.Background())
	w = request(handler, http.MethodGet, "/inputs/apps", "")
	requireError(t, w, 503, "source_unavailable")
	if strings.Contains(w.Body.String(), "credential-secret") {
		t.Fatal("secret exposed")
	}
	current = github.Result{Repositories: []discovery.Repository{}}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	w = request(handler, http.MethodGet, "/inputs/apps", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"inputs":[]}` {
		t.Fatalf("successful empty output = %d %s", w.Code, w.Body)
	}
	requireError(t, request(handler, http.MethodGet, "/inputs/unknown", ""), 404, "not_found")
}

func TestDryRunHTTPValidationAndCompleteReport(t *testing.T) {
	calls := 0
	s := newService(t, scannerFunc(func(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		calls++
		out := map[config.SourceName]github.Result{}
		for name := range sources {
			out[name] = github.Result{Repositories: []discovery.Repository{}}
		}
		return out
	}))
	handler := httpapi.New(s, httpapi.Options{MaxBodyBytes: 2048})
	validConfig, err := json.Marshal(apiConfig())
	if err != nil {
		t.Fatal(err)
	}
	validBody := `{"config":` + string(validConfig) + `}`
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
	}{
		{"malformed", "{", 400, "invalid_request"},
		{"unknown envelope", `{"config":` + string(validConfig) + `,"credentials":{}}`, 400, "invalid_request"},
		{"duplicate envelope", `{"config":` + string(validConfig) + `,"config":` + string(validConfig) + `}`, 400, "invalid_request"},
		{"trailing document", validBody + "{}", 400, "invalid_request"},
		{"missing config", `{}`, 400, "invalid_request"},
		{"null config", `{"config":null}`, 422, "invalid_config"},
		{"unknown config field", `{"config":{"version":1,"privateKeyFile":"/server/secret"}}`, 422, "invalid_config"},
		{"unsupported version", strings.Replace(validBody, `"version":1`, `"version":2`, 1), 422, "invalid_config"},
		{"unknown credentials", strings.Replace(validBody, `"credential":"token"`, `"credential":"missing"`, 1), 422, "invalid_config"},
		{"duplicate config field", strings.Replace(validBody, `"version":1`, `"version":1,"version":1`, 1), 400, "invalid_request"},
		{"oversize", strings.Repeat(" ", 2049) + validBody, 413, "request_too_large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(handler, http.MethodPost, "/api/v1/dry-run", tc.body)
			requireError(t, w, tc.status, tc.code)
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("dry-run errors may be cached")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("invalid input triggered %d network scans", calls)
	}
	w := request(handler, http.MethodPost, "/api/v1/dry-run", validBody)
	if w.Code != 200 {
		t.Fatalf("dry-run HTTP %d: %s", w.Code, w.Body)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("dry-run success may be cached")
	}
	var report service.Report
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Version != 1 || report.BaseRevision == "" || report.CandidateRevision == "" || report.Filters["apps"].AfterCount != 0 || calls != 1 {
		t.Fatalf("incomplete report: %#v calls=%d", report, calls)
	}
	requireError(t, request(handler, http.MethodGet, "/inputs/apps", ""), 503, "not_ready")
}

func TestStatusLivenessAndReadinessHaveSeparateMeanings(t *testing.T) {
	s := newService(t, scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		return map[config.SourceName]github.Result{"company": {Err: errors.New("down")}}
	}))
	handler := httpapi.New(s, httpapi.Options{})
	if w := request(handler, http.MethodGet, "/healthz", ""); w.Code != 200 {
		t.Fatalf("healthy process HTTP %d", w.Code)
	}
	if w := request(handler, http.MethodGet, "/readyz", ""); w.Code != 503 {
		t.Fatalf("not started process readiness HTTP %d", w.Code)
	}
	_ = s.Scan(context.Background())
	w := request(handler, http.MethodGet, "/api/v1/status", "")
	if w.Code != 200 {
		t.Fatalf("status HTTP %d", w.Code)
	}
	var status service.Status
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Generation == 0 || status.Sources["company"].Ready || status.Filters["apps"].Ready {
		t.Fatalf("status lost failure: %#v", status)
	}
	if w = request(handler, http.MethodPost, "/inputs/apps", ""); w.Code != 405 {
		t.Fatalf("invalid method HTTP %d", w.Code)
	}
	if w = request(handler, http.MethodGet, "/api/v1/dry-run", ""); w.Code != 405 {
		t.Fatalf("invalid method HTTP %d", w.Code)
	}
}

func TestMiddlewareCanProtectOperatorAndInputsSeparately(t *testing.T) {
	s := newService(t, scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		return nil
	}))
	mark := func(header string) httpapi.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set(header, "yes"); next.ServeHTTP(w, r) })
		}
	}
	handler := httpapi.New(s, httpapi.Options{Middleware: []httpapi.Middleware{mark("X-All")}, InputsMiddleware: []httpapi.Middleware{mark("X-Inputs")}, OperatorMiddleware: []httpapi.Middleware{mark("X-Operator")}})
	for _, tc := range []struct{ path, expected, absent string }{{"/inputs/apps", "X-Inputs", "X-Operator"}, {"/api/v1/status", "X-Operator", "X-Inputs"}} {
		w := request(handler, http.MethodGet, tc.path, "")
		if w.Header().Get("X-All") != "yes" || w.Header().Get(tc.expected) != "yes" || w.Header().Get(tc.absent) != "" {
			t.Fatalf("middleware routing for %s: %v", tc.path, w.Header())
		}
	}
}
