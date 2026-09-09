package discovery_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
)

func mustFilter(t testing.TB, spec config.FilterSpec) config.Filter {
	t.Helper()
	sources := make(map[config.SourceName]config.Source)
	for _, name := range spec.Sources {
		sources[name] = config.Source{Owners: []string{"Acme"}, Credential: "token"}
	}
	cfg, err := config.Parse(config.Document{
		Version: 1,
		Server:  config.Server{Listen: ":8080"},
		Scan:    config.Scan{Interval: config.Duration(10 * time.Minute), Timeout: config.Duration(2 * time.Minute)},
		Sources: sources,
		Filters: map[config.FilterName]config.FilterSpec{"test": spec},
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Filters()["test"]
}

func TestEvaluateRejectsUninitializedPreparedFilterWithoutEmptySuccess(t *testing.T) {
	var filter config.Filter
	for _, catalogs := range []map[config.SourceName][]discovery.Repository{nil, {}, {"s": {repo(1, "api")}}} {
		result, err := discovery.Evaluate(filter, catalogs)
		if err == nil || len(result.Inputs) != 0 {
			t.Fatalf("uninitialized filter produced successful/partial result: %#v %v", result, err)
		}
	}
}

func TestEvaluatePreparedFilterSurvivesSourceAndPredicateInputMutation(t *testing.T) {
	spec := config.FilterSpec{
		Sources: []config.SourceName{"s"},
		Include: []config.Rule{{NameRegex: "^service-", Topics: &config.Topics{All: []string{"gitops"}}}},
		Exclude: []config.Rule{{Repositories: []string{"Acme/service-sandbox"}}},
	}
	filter := mustFilter(t, spec)
	spec.Sources[0] = "missing"
	spec.Include[0].NameRegex = "["
	spec.Include[0].Topics.All[0] = "changed"
	spec.Exclude[0].Repositories[0] = "Acme/service-api"
	filter.Sources()[0] = "unavailable"
	filter.Repositories()[0] = "Acme/service-api"
	got, err := discovery.Evaluate(filter, map[config.SourceName][]discovery.Repository{
		"s": {repo(1, "service-api", "gitops"), repo(2, "service-sandbox", "gitops"), repo(3, "other", "gitops"), repo(4, "service-no-topic")},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []discovery.Input{{ID: "1", FullName: "Acme/service-api", Owner: "Acme", Name: "service-api"}}
	if !reflect.DeepEqual(got.Inputs, want) {
		t.Fatalf("prepared filter changed after caller mutation: got %#v want %#v", got.Inputs, want)
	}
}
