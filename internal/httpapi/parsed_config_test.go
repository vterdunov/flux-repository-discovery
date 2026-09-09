package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/httpapi"
)

func TestParsedConfigJSONDuplicatesAreRequestErrorsBeforeScanning(t *testing.T) {
	calls := 0
	s := newService(t, scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		calls++
		return nil
	}))
	wire, err := json.Marshal(apiConfig())
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"config":` + string(wire) + `}`
	for _, tc := range []struct{ name, body string }{
		{"candidate root", strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1)},
		{"deep duplicate", strings.Replace(valid, `"nameRegex":".*"`, `"nameRegex":".*","nameRegex":"^api$"`, 1)},
		{"escaped deep duplicate", strings.Replace(valid, `"nameRegex":".*"`, `"nameRegex":".*","name\u0052egex":"^api$"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(httpapi.New(s, httpapi.Options{}), http.MethodPost, "/api/v1/dry-run", tc.body)
			requireError(t, w, http.StatusBadRequest, "invalid_request")
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("invalid request can be cached")
			}
		})
	}
	if calls != 0 {
		t.Fatalf("invalid JSON triggered %d scans", calls)
	}
}

func TestParsedConfigContentErrorsRemainConfigurationErrors(t *testing.T) {
	calls := 0
	s := newService(t, scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		calls++
		return nil
	}))
	wire, err := json.Marshal(apiConfig())
	if err != nil {
		t.Fatal(err)
	}
	valid := `{"config":` + string(wire) + `}`
	for _, tc := range []struct{ name, body string }{
		{"regexp syntax", strings.Replace(valid, `"nameRegex":".*"`, `"nameRegex":"["`, 1)},
		{"source reference", strings.Replace(valid, `"sources":["company"]`, `"sources":["missing"]`, 1)},
		{"credential reference", strings.Replace(valid, `"credential":"token"`, `"credential":"missing"`, 1)},
		{"duration", strings.Replace(valid, `"interval":"10m0s"`, `"interval":"never"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(httpapi.New(s, httpapi.Options{}), http.MethodPost, "/api/v1/dry-run", tc.body)
			requireError(t, w, http.StatusUnprocessableEntity, "invalid_config")
		})
	}
	if calls != 0 {
		t.Fatalf("invalid configuration triggered %d scans", calls)
	}
}

func TestParsedConfigServerOverrideCannotHideInvalidCandidate(t *testing.T) {
	calls := 0
	s := newService(t, scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		calls++
		return nil
	}), map[string]string{"FRD_SCAN_INTERVAL": "1m"})
	wire, err := json.Marshal(apiConfig())
	if err != nil {
		t.Fatal(err)
	}
	body := `{"config":` + strings.Replace(string(wire), `"interval":"10m0s"`, `"interval":"0s"`, 1) + `}`
	w := request(httpapi.New(s, httpapi.Options{}), http.MethodPost, "/api/v1/dry-run", body)
	requireError(t, w, http.StatusUnprocessableEntity, "invalid_config")
	if calls != 0 {
		t.Fatalf("invalid declared configuration reached %d scans despite environment override", calls)
	}
}
