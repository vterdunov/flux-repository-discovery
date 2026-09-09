package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/cli"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

func TestPartialSuccessfulReportCannotLookLikeCompletedDryRun(t *testing.T) {
	path := writeConfig(t, configYAML)
	for _, tc := range []struct {
		name   string
		mutate func(*service.Report)
	}{
		{"observation time missing", func(report *service.Report) { report.ObservedAt = time.Time{} }},
		{"complete proposed inputs missing", func(report *service.Report) {
			diff := report.Filters["apps"]
			diff.Inputs = nil
			report.Filters["apps"] = diff
		}},
		{"negative counts", func(report *service.Report) {
			diff := report.Filters["apps"]
			diff.BeforeCount = -1
			report.Filters["apps"] = diff
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := successReport()
			tc.mutate(&report)
			wire, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			opts := options(&stdout, &stderr)
			opts.HTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return response(200, string(wire)), nil })}
			code := cli.Run(context.Background(), []string{"dry-run", "--config", path, "--against", "https://discovery.example", "--output", "json"}, opts)
			if code != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("partial report presented as successful dry-run: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
		})
	}
}

func TestDryRunDoesNotFollowRedirectWithCandidateConfiguration(t *testing.T) {
	path := writeConfig(t, configYAML)
	calls := 0
	var stdout, stderr bytes.Buffer
	opts := options(&stdout, &stderr)
	opts.HTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "discovery.example" {
			t.Fatal("candidate was sent to redirect target")
		}
		res := response(http.StatusTemporaryRedirect, "")
		res.Header.Set("Location", "https://other.example/collect")
		return res, nil
	})}
	code := cli.Run(context.Background(), []string{"dry-run", "--config", path, "--against", "https://discovery.example"}, opts)
	if code != 1 || calls != 1 || stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("redirect behavior: code=%d calls=%d stdout=%s stderr=%s", code, calls, &stdout, &stderr)
	}
}
