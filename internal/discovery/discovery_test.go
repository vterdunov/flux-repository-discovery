package discovery_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
)

func repo(id discovery.RepositoryID, name string, topics ...string) discovery.Repository {
	return discovery.Repository{ID: id, Owner: "Acme", Name: name, Topics: topics}
}

func TestEvaluateCombinesRulesAndExclusions(t *testing.T) {
	filter := config.FilterSpec{
		Sources: []config.SourceName{"company"},
		Include: []config.Rule{
			{Topics: &config.Topics{All: []string{"gitops", "managed"}, Any: []string{"backend", "worker"}}, NameRegex: "^service-"},
			{Repositories: []string{"acme/legacy", "ACME/ARCHIVED", "Acme/fork", "acme/missing"}},
		},
		Exclude: []config.Rule{{NameRegex: "sandbox"}, {Repositories: []string{"ACME/blocked"}}},
	}
	catalog := []discovery.Repository{
		repo(100, "service-api", "GitOps", "managed", "BACKEND"),
		repo(2, "service-worker", "gitops", "managed", "worker"),
		repo(3, "service-no-all", "gitops", "backend"),
		repo(4, "service-no-any", "gitops", "managed", "frontend"),
		repo(5, "not-service", "gitops", "managed", "worker"),
		repo(6, "legacy"),
		repo(7, "service-sandbox", "gitops", "managed", "backend"),
		{ID: 8, Owner: "Acme", Name: "archived", Archived: true},
		{ID: 9, Owner: "Acme", Name: "fork", Fork: true},
	}
	got, err := discovery.Evaluate(mustFilter(t, filter), map[config.SourceName][]discovery.Repository{"company": catalog})
	if err != nil {
		t.Fatal(err)
	}
	want := []discovery.Input{
		{ID: "2", FullName: "Acme/service-worker", Owner: "Acme", Name: "service-worker"},
		{ID: "6", FullName: "Acme/legacy", Owner: "Acme", Name: "legacy"},
		{ID: "100", FullName: "Acme/service-api", Owner: "Acme", Name: "service-api"},
	}
	if !reflect.DeepEqual(got.Inputs, want) {
		t.Fatalf("inputs = %#v, want %#v", got.Inputs, want)
	}
	// Diagnostics include references in includes and exclusions, sorted lexically.
	if !reflect.DeepEqual(got.UnmatchedRepositories, []string{"acme/blocked", "acme/missing"}) {
		t.Fatalf("unmatched = %#v", got.UnmatchedRepositories)
	}
}

func TestEvaluateConditionSemantics(t *testing.T) {
	cases := []struct {
		name       string
		rule       config.Rule
		repository discovery.Repository
		match      bool
	}{
		{"repo and regex both required", config.Rule{Repositories: []string{"acme/api"}, NameRegex: "^web"}, repo(1, "api"), false},
		{"repo and topic both required", config.Rule{Repositories: []string{"acme/api"}, Topics: &config.Topics{All: []string{"gitops"}}}, repo(1, "api"), false},
		{"all topics required", config.Rule{Topics: &config.Topics{All: []string{"a", "b"}}}, repo(1, "api", "a"), false},
		{"any topics OR", config.Rule{Topics: &config.Topics{Any: []string{"a", "b"}}}, repo(1, "api", "b"), true},
		{"topic case insensitive", config.Rule{Topics: &config.Topics{All: []string{"GiToPs"}}}, repo(1, "api", "gitops"), true},
		{"repo case insensitive", config.Rule{Repositories: []string{"ACME/API"}}, repo(1, "api"), true},
		{"repo exact", config.Rule{Repositories: []string{"acme/api"}}, repo(1, "api-worker"), false},
		{"regex short name only", config.Rule{NameRegex: "^Acme/"}, repo(1, "api"), false},
		{"regex case sensitive", config.Rule{NameRegex: "^API$"}, repo(1, "api"), false},
		{"regex explicit insensitive", config.Rule{NameRegex: "(?i)^API$"}, repo(1, "api"), true},
		{"regex substring", config.Rule{NameRegex: "api"}, repo(1, "web-api-v2"), true},
		{"regex explicit all", config.Rule{NameRegex: ".*"}, repo(1, "api"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := discovery.Evaluate(mustFilter(t, config.FilterSpec{Sources: []config.SourceName{"s"}, Include: []config.Rule{tc.rule}}), map[config.SourceName][]discovery.Repository{"s": {tc.repository}})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(result.Inputs) == 1; got != tc.match {
				t.Fatalf("match = %v, want %v", got, tc.match)
			}
		})
	}
}

func TestExcludeAlwaysWinsAndUsesANDWithinRule(t *testing.T) {
	filter := config.FilterSpec{Sources: []config.SourceName{"s"}, Include: []config.Rule{{Repositories: []string{"Acme/api", "Acme/web"}}}, Exclude: []config.Rule{{NameRegex: "api", Topics: &config.Topics{All: []string{"blocked"}}}}}
	got, err := discovery.Evaluate(mustFilter(t, filter), map[config.SourceName][]discovery.Repository{"s": {repo(1, "api", "blocked"), repo(2, "web", "blocked"), repo(3, "api")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Inputs) != 2 || got.Inputs[0].ID != "2" || got.Inputs[1].ID != "3" {
		t.Fatalf("inputs = %#v", got.Inputs)
	}
}

func TestDeduplicateAcrossSourcesAndIgnoreUnreferencedCatalogs(t *testing.T) {
	a := repo(10, "api", "a", "b")
	b := repo(10, "api", "b", "a")
	filter := config.FilterSpec{Sources: []config.SourceName{"first", "second"}, Include: []config.Rule{{NameRegex: ".*"}}}
	got, err := discovery.Evaluate(mustFilter(t, filter), map[config.SourceName][]discovery.Repository{"first": {a}, "second": {b, repo(2, "worker")}, "unrelated": {repo(4, "other")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Inputs) != 2 || got.Inputs[0].ID != "2" || got.Inputs[1].ID != "10" {
		t.Fatalf("inputs = %#v", got.Inputs)
	}
}

func TestConflictingMetadataFailsEntireDependentResult(t *testing.T) {
	base := repo(1, "api", "gitops")
	cases := []struct {
		name   string
		mutate func(*discovery.Repository)
	}{
		{"renamed", func(r *discovery.Repository) { r.Name = "renamed" }},
		{"owner", func(r *discovery.Repository) { r.Owner = "Other" }},
		{"topics", func(r *discovery.Repository) { r.Topics = []string{"different"} }},
		{"archived", func(r *discovery.Repository) { r.Archived = true }},
		{"fork", func(r *discovery.Repository) { r.Fork = true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			other := base
			tc.mutate(&other)
			got, err := discovery.Evaluate(mustFilter(t, config.FilterSpec{Sources: []config.SourceName{"a", "b"}, Include: []config.Rule{{NameRegex: ".*"}}}), map[config.SourceName][]discovery.Repository{"a": {base, repo(2, "good")}, "b": {other}})
			if err == nil {
				t.Fatal("conflicting catalog accepted")
			}
			if len(got.Inputs) != 0 {
				t.Fatalf("partial result on error: %#v", got)
			}
		})
	}
}

func TestEmptyResultSerializesAsArrayAndMissingSourceFails(t *testing.T) {
	filter := config.FilterSpec{Sources: []config.SourceName{"s"}, Include: []config.Rule{{NameRegex: ".*"}}}
	got, err := discovery.Evaluate(mustFilter(t, filter), map[config.SourceName][]discovery.Repository{"s": {}})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(struct {
		Inputs []discovery.Input `json:"inputs"`
	}{got.Inputs})
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != `{"inputs":[]}` {
		t.Fatalf("empty wire response: %s", wire)
	}
	got, err = discovery.Evaluate(mustFilter(t, filter), map[config.SourceName][]discovery.Repository{})
	if err == nil || len(got.Inputs) != 0 {
		t.Fatalf("missing catalog treated as empty success: %#v %v", got, err)
	}
}

func TestInvalidRequiredRepositoryDataNeverPublishes(t *testing.T) {
	for _, bad := range []discovery.Repository{{ID: 0, Owner: "Acme", Name: "api"}, {ID: -1, Owner: "Acme", Name: "api"}, {ID: 1, Name: "api"}, {ID: 1, Owner: "Acme"}} {
		got, err := discovery.Evaluate(mustFilter(t, config.FilterSpec{Sources: []config.SourceName{"s"}, Include: []config.Rule{{NameRegex: ".*"}}}), map[config.SourceName][]discovery.Repository{"s": {repo(2, "valid"), bad}})
		if err == nil || len(got.Inputs) != 0 {
			t.Fatalf("invalid repository %+v gave partial/success result %+v, %v", bad, got, err)
		}
	}
}

func TestEvaluateDoesNotMutateCatalogAndRenameKeepsStableID(t *testing.T) {
	filter := config.FilterSpec{Sources: []config.SourceName{"s"}, Include: []config.Rule{{NameRegex: ".*"}}}
	catalog := map[config.SourceName][]discovery.Repository{"s": {repo(2, "second", "b", "a"), repo(1, "first")}}
	before, _ := json.Marshal(catalog)
	old, err := discovery.Evaluate(mustFilter(t, filter), catalog)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(catalog)
	if string(before) != string(after) {
		t.Fatal("Evaluate mutated caller catalog")
	}
	catalog["s"][1].Name = "renamed"
	updated, err := discovery.Evaluate(mustFilter(t, filter), catalog)
	if err != nil {
		t.Fatal(err)
	}
	if old.Inputs[0].ID != updated.Inputs[0].ID || old.Inputs[0].Name != "first" || updated.Inputs[0].Name != "renamed" {
		t.Fatalf("rename lost stable identity: %#v -> %#v", old, updated)
	}
}

func FuzzEvaluateNeverPublishesArchivedOrForked(f *testing.F) {
	f.Add("api", true, false)
	f.Add("worker", false, true)
	f.Add("plain", false, false)
	f.Fuzz(func(t *testing.T, name string, archived, fork bool) {
		if name == "" || strings.ContainsAny(name, "/\x00") {
			return
		}
		got, err := discovery.Evaluate(mustFilter(t, config.FilterSpec{Sources: []config.SourceName{"s"}, Include: []config.Rule{{NameRegex: ".*"}}}), map[config.SourceName][]discovery.Repository{"s": {{ID: 1, Owner: "Acme", Name: name, Archived: archived, Fork: fork}}})
		if err != nil {
			return
		}
		if (archived || fork) && len(got.Inputs) != 0 {
			t.Fatal("excluded repository was published")
		}
		if len(got.Inputs) > 1 {
			t.Fatal("single repository duplicated")
		}
	})
}
