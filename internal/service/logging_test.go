package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

// Diagnostics are a consumer contract: operators need the affected source,
// GitHub request ID and per-filter count without arbitrary upstream text.
func TestScanLogsSafeSourceDiagnosticsAndFilterCounts(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	cfg := baseConfig()
	cfg.Sources["personal"] = config.Source{Owners: []string{"bob"}, Credential: "token2"}
	cfg.Filters["personal"] = config.FilterSpec{Sources: []config.SourceName{"personal"}, Include: []config.Rule{{NameRegex: ".*"}}}
	scan := scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		return map[config.SourceName]github.Result{
			"company":  {Err: fmt.Errorf("authorization=must-never-be-logged: %w", &github.Error{Code: "rate_limited", StatusCode: 429, RequestID: "ABCD:1234"})},
			"personal": {Repositories: []discovery.Repository{{ID: 2, Owner: "bob", Name: "repo"}}},
		}
	})
	s := mustService(t, cfg, scan, service.Options{})
	if err := s.Scan(context.Background()); err == nil {
		t.Fatal("source failure was hidden")
	}
	if strings.Contains(output.String(), "must-never-be-logged") {
		t.Fatal("arbitrary scanner error text leaked into structured logs")
	}
	var sourceFailure, sourceSuccess, filterFailure, filterSuccess bool
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		var record map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("non-JSON log record: %v", err)
		}
		field := func(name string) string { var value string; _ = json.Unmarshal(record[name], &value); return value }
		integer := func(name string) int { var value int; _ = json.Unmarshal(record[name], &value); return value }
		if field("source") == "company" {
			sourceFailure = field("error_code") == "rate_limited" && integer("github_status") == 429 && field("github_request_id") == "ABCD:1234" && integer("generation") == 1 && record["duration"] != nil
		}
		if field("source") == "personal" {
			sourceSuccess = integer("count") == 1 && integer("generation") == 1 && record["duration"] != nil
		}
		if field("filter") == "apps" {
			filterFailure = field("error_code") == "source_unavailable" && integer("generation") == 1
		}
		if field("filter") == "personal" {
			filterSuccess = integer("count") == 1 && integer("generation") == 1
		}
	}
	if !sourceFailure || !sourceSuccess || !filterFailure || !filterSuccess {
		t.Fatalf("missing structured diagnostics: sourceFailure=%t sourceSuccess=%t filterFailure=%t filterSuccess=%t\n%s", sourceFailure, sourceSuccess, filterFailure, filterSuccess, &output)
	}
}
