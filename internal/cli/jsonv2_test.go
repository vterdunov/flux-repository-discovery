package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/cli"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

func TestJSONMigrationRejectsAmbiguousOrInvalidUTF8SuccessReports(t *testing.T) {
	path := writeConfig(t, configYAML)
	wire, err := json.Marshal(successReport())
	if err != nil {
		t.Fatal(err)
	}
	valid := string(wire)
	for _, tc := range []struct {
		name, body string
	}{
		{"duplicate version", strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1)},
		{"escaped duplicate version", strings.Replace(valid, `"version":1`, `"version":1,"\u0076ersion":1`, 1)},
		{"nested duplicate count", strings.Replace(valid, `"afterCount":2`, `"afterCount":2,"afterCount":2`, 1)},
		{"invalid UTF8 revision", strings.Replace(valid, `"base-revision"`, "\"base-\xffrevision\"", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			opts := options(&stdout, &stderr)
			opts.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(http.StatusOK, tc.body), nil
			})}
			code := cli.Run(context.Background(), []string{"dry-run", "--config", path, "--against", "https://discovery.example", "--output", "json"}, opts)
			if code != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("invalid JSON report presented as successful: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
		})
	}
}

func TestJSONMigrationCLIReportRetainsEmptyObjectsNullsAndZeroValues(t *testing.T) {
	path := writeConfig(t, configYAML)
	report := successReport()
	report.ConfigChanges = append(report.ConfigChanges,
		service.Change{Operation: "replace", Path: "extension.emptyObject", Before: json.RawMessage(`{}`), After: json.RawMessage(`{}`)},
		service.Change{Operation: "replace", Path: "extension.emptyArray", Before: json.RawMessage(`[]`), After: json.RawMessage(`[]`)},
		service.Change{Operation: "replace", Path: "extension.false", Before: json.RawMessage(`false`), After: json.RawMessage(`false`)},
		service.Change{Operation: "replace", Path: "extension.zero", Before: json.RawMessage(`0`), After: json.RawMessage(`0`)})
	report.Overrides = map[string]string{}
	wire, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	opts := options(&stdout, &stderr)
	opts.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusOK, string(wire)), nil
	})}
	code := cli.Run(context.Background(), []string{"dry-run", "--config", path, "--against", "https://discovery.example", "--output", "json"}, opts)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("valid report rejected: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	// Compare the external JSON values, including null versus [] and missing
	// versus explicit {}, instead of relying on either encoder's formatting.
	var before, after any
	if err := json.Unmarshal(wire, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("CLI changed report JSON values\nbefore: %s\nafter: %s", wire, &stdout)
	}
}
