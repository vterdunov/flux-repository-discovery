package service_test

import (
	"context"
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

func TestJSONMigrationPreservesRevisionAcrossEncodingAndMapOrder(t *testing.T) {
	// This pre-migration revision is part of the public status/report contract.
	// The configuration deliberately exercises '<' and '&' JSON escaping.
	const revision = "4d35272db8411a2084633b82af16f6dd2a004696f5ce1ac95db4cb857ec876f2"
	for iteration := 0; iteration < 20; iteration++ {
		cfg := baseConfig()
		cfg.Sources = make(map[config.SourceName]config.Source)
		cfg.Filters = make(map[config.FilterName]config.FilterSpec)
		sourceNames := []config.SourceName{"company", "personal"}
		filterNames := []config.FilterName{"apps", "platform"}
		if iteration%2 != 0 {
			sourceNames[0], sourceNames[1] = sourceNames[1], sourceNames[0]
			filterNames[0], filterNames[1] = filterNames[1], filterNames[0]
		}
		for _, name := range sourceNames {
			if name == "company" {
				cfg.Sources[name] = config.Source{Owners: []string{"acme"}, Credential: "token"}
			} else {
				cfg.Sources[name] = config.Source{Owners: []string{"bob"}, Credential: "token2"}
			}
		}
		for _, name := range filterNames {
			if name == "apps" {
				cfg.Filters[name] = config.FilterSpec{Sources: []config.SourceName{"company"}, Include: []config.Rule{{NameRegex: "^[<&]"}}}
			} else {
				cfg.Filters[name] = config.FilterSpec{Sources: []config.SourceName{"personal"}, Include: []config.Rule{{NameRegex: "^infra-"}}}
			}
		}
		s := mustService(t, cfg, scannerFunc(func(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
			out := make(map[config.SourceName]github.Result)
			for name := range sources {
				out[name] = github.Result{Repositories: []discovery.Repository{}}
			}
			return out
		}), service.Options{})
		if got := s.Status().Revision; got != revision {
			t.Fatalf("iteration %d: revision changed without a config change: got %s, want %s", iteration, got, revision)
		}
		report, err := s.DryRun(context.Background(), mustConfig(t, cfg))
		if err != nil {
			t.Fatal(err)
		}
		if report.BaseRevision != revision || report.CandidateRevision != revision || report.HasChanges || len(report.ConfigChanges) != 0 || len(report.EffectiveChanges) != 0 {
			t.Fatalf("equivalent config produced a different revision or false changes: %#v", report)
		}
	}
}
