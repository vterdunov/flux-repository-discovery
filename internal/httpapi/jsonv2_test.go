package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/httpapi"
)

func TestJSONMigrationDryRunRejectsInvalidUTF8BeforeScanning(t *testing.T) {
	calls := 0
	s := newService(t, scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		calls++
		return nil
	}))
	handler := httpapi.New(s, httpapi.Options{})
	wire, err := json.Marshal(apiConfig())
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"config":` + string(wire) + `}`
	for _, tc := range []struct {
		name, body string
	}{
		{"invalid UTF8 value", strings.Replace(valid, `"nameRegex":".*"`, "\"nameRegex\":\"bad-\xff\"", 1)},
		{"invalid UTF8 key", strings.Replace(valid, `"nameRegex"`, "\"name\xffRegex\"", 1)},
		{"escaped duplicate envelope", `{"config":` + string(wire) + `,"\u0063onfig":` + string(wire) + `}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireError(t, request(handler, http.MethodPost, "/api/v1/dry-run", tc.body), http.StatusBadRequest, "invalid_request")
		})
	}
	if calls != 0 {
		t.Fatalf("malformed JSON triggered %d scans", calls)
	}
}

func TestJSONMigrationHTTPKeepsUnavailableDriftDistinctFromEmptyResult(t *testing.T) {
	s := newService(t, scannerFunc(func(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		out := make(map[config.SourceName]github.Result)
		for name := range sources {
			out[name] = github.Result{Repositories: []discovery.Repository{}}
		}
		return out
	}))
	handler := httpapi.New(s, httpapi.Options{})
	candidate, err := json.Marshal(apiConfig())
	if err != nil {
		t.Fatal(err)
	}
	w := request(handler, http.MethodPost, "/api/v1/dry-run", `{"config":`+string(candidate)+`}`)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body)
	}
	report := jsonObject(t, w.Body.Bytes())
	for field, want := range map[string]string{"version": "1", "baseGeneration": "0", "hasChanges": "false", "configChanges": "[]", "effectiveChanges": "[]", "overrides": "{}"} {
		requireJSONValue(t, report, field, want)
	}
	filters := jsonObject(t, report["filters"])
	diff := jsonObject(t, filters["apps"])
	for field, want := range map[string]string{"kind": `"unchanged"`, "beforeCount": "0", "afterCount": "0", "unchanged": "0", "added": "[]", "removed": "[]", "changed": "[]", "inputs": "[]", "unmatchedRepositories": "[]"} {
		requireJSONValue(t, diff, field, want)
	}
	drift := jsonObject(t, report["drift"])
	appsDrift := jsonObject(t, drift["apps"])
	requireJSONValue(t, appsDrift, "available", "false")
	unavailable := jsonObject(t, appsDrift["diff"])
	for field, want := range map[string]string{"kind": `""`, "beforeCount": "0", "afterCount": "0", "unchanged": "0", "added": "null", "removed": "null", "changed": "null", "inputs": "null", "unmatchedRepositories": "null"} {
		requireJSONValue(t, unavailable, field, want)
	}
	status := request(handler, http.MethodGet, "/api/v1/status", "")
	statusJSON := jsonObject(t, status.Body.Bytes())
	requireJSONValue(t, statusJSON, "generation", "0")
	statusFilters := jsonObject(t, statusJSON["filters"])
	appsStatus := jsonObject(t, statusFilters["apps"])
	requireJSONValue(t, appsStatus, "ready", "false")
	requireJSONValue(t, appsStatus, "count", "0")
}

func TestJSONMigrationHTTPDurationChangesRemainStrings(t *testing.T) {
	s := newService(t, scannerFunc(func(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		out := make(map[config.SourceName]github.Result)
		for name := range sources {
			out[name] = github.Result{Repositories: []discovery.Repository{}}
		}
		return out
	}))
	wire, err := json.Marshal(apiConfig())
	if err != nil {
		t.Fatal(err)
	}
	candidate := strings.Replace(string(wire), `"interval":"10m0s"`, `"interval":"1m30s"`, 1)
	candidate = strings.Replace(candidate, `"timeout":"2m0s"`, `"timeout":"3.5s"`, 1)
	w := request(httpapi.New(s, httpapi.Options{}), http.MethodPost, "/api/v1/dry-run", `{"config":`+candidate+`}`)
	if w.Code != http.StatusOK {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body)
	}
	report := jsonObject(t, w.Body.Bytes())
	for _, group := range []string{"configChanges", "effectiveChanges"} {
		var changes []map[string]json.RawMessage
		if err := json.Unmarshal(report[group], &changes); err != nil {
			t.Fatal(err)
		}
		if len(changes) != 2 {
			t.Fatalf("%s lost duration changes: %s", group, report[group])
		}
		requireJSONValue(t, changes[0], "path", `"scan.interval"`)
		requireJSONValue(t, changes[0], "before", `"10m0s"`)
		requireJSONValue(t, changes[0], "after", `"1m30s"`)
		requireJSONValue(t, changes[1], "path", `"scan.timeout"`)
		requireJSONValue(t, changes[1], "before", `"2m0s"`)
		requireJSONValue(t, changes[1], "after", `"3.5s"`)
	}
}

func jsonObject(t *testing.T, data []byte) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		t.Fatalf("expected JSON object: %s (%v)", data, err)
	}
	return object
}

func requireJSONValue(t *testing.T, object map[string]json.RawMessage, field, want string) {
	t.Helper()
	got, exists := object[field]
	if !exists || string(got) != want {
		t.Errorf("field %s: got %s (present %t), want %s", field, got, exists, want)
	}
}
