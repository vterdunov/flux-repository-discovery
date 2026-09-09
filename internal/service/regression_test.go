package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

func TestTypedConfigCannotLoseInvalidEmptyConditionDuringNormalization(t *testing.T) {
	candidate := baseConfig()
	candidate.Filters["apps"] = config.FilterSpec{Sources: []config.SourceName{"company"}, Include: []config.Rule{{Topics: &config.Topics{All: []string{}, Any: []string{"gitops"}}}}}
	// Invalid documents are rejected by the config parser before either
	// the service constructor or DryRun can receive a prepared Config.
	_, err := config.Parse(candidate)
	if err == nil || !strings.Contains(err.Error(), "topics.all") {
		t.Fatalf("parser normalized away an explicitly empty topics.all instead of rejecting it: %v", err)
	}
}

func TestSourceTimeoutDoesNotDiscardAnotherCompletedSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := baseConfig()
		cfg.Scan.Timeout = config.Duration(time.Second)
		cfg.Sources["personal"] = config.Source{Owners: []string{"bob"}, Credential: "token2"}
		cfg.Filters["personal"] = config.FilterSpec{Sources: []config.SourceName{"personal"}, Include: []config.Rule{{NameRegex: ".*"}}}
		scan := scannerFunc(func(ctx context.Context, _ map[config.SourceName]config.Source) map[config.SourceName]github.Result {
			// B completes while the whole-cycle context is still live. A then
			// exhausts the remaining deadline and reports its own failure.
			results := map[config.SourceName]github.Result{"personal": {Repositories: []discovery.Repository{{ID: 2, Owner: "bob", Name: "complete"}}}}
			<-ctx.Done()
			results["company"] = github.Result{Err: ctx.Err()}
			return results
		})
		s := mustService(t, cfg, scan, service.Options{})
		if err := s.Scan(context.Background()); err == nil {
			t.Fatal("timed-out source was not reported")
		}
		_, err := s.Inputs("apps")
		requireCode(t, err, "source_unavailable")
		inputs, err := s.Inputs("personal")
		if err != nil || len(inputs) != 1 || inputs[0].FullName != "bob/complete" {
			t.Fatalf("completed independent source discarded after another source timeout: %#v %v", inputs, err)
		}
	})
}

func TestOnlySupportedServerOverridesAppearInDryRunReport(t *testing.T) {
	s := mustService(t, baseConfig(), scannerFunc(func(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		results := map[config.SourceName]github.Result{}
		for name := range sources {
			results[name] = github.Result{Repositories: []discovery.Repository{}}
		}
		return results
	}), service.Options{}, map[string]string{"FRD_SCAN_INTERVAL": "30s", "PRIVATE_TOKEN": "do-not-publish", "FRD_GITHUB_TOKEN": "do-not-publish"})
	report, err := s.DryRun(context.Background(), mustConfig(t, baseConfig()))
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(wire), "do-not-publish") || len(report.Overrides) != 1 || report.Overrides["FRD_SCAN_INTERVAL"] != "30s" {
		t.Fatalf("report includes unsupported server environment: %#v", report.Overrides)
	}
}
