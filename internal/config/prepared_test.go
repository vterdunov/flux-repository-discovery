package config_test

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
)

func preparedDocument() config.Document {
	return config.Document{
		Version: 1,
		Server:  config.Server{Listen: ":8080"},
		Scan:    config.Scan{Interval: config.Duration(10 * time.Minute), Timeout: config.Duration(2 * time.Minute)},
		Sources: map[config.SourceName]config.Source{
			"company": {Owners: []string{"Acme", "Other"}, Credential: "company-app"},
		},
		Filters: map[config.FilterName]config.FilterSpec{
			"apps": {
				Sources: []config.SourceName{"company"},
				Include: []config.Rule{
					{Topics: &config.Topics{All: []string{"GitOps", "managed"}, Any: []string{"Worker", "backend"}}, Repositories: []string{"Acme/service-api", "Acme/service-worker"}, NameRegex: "^service-"},
					{Repositories: []string{"Acme/legacy"}},
				},
				Exclude: []config.Rule{{Topics: &config.Topics{All: []string{"Blocked"}, Any: []string{"Sandbox", "test"}}, Repositories: []string{"Acme/service-api"}, NameRegex: "api$"}},
			},
		},
	}
}

// This deliberately reaches every mutable layer of the public document schema.
func mutatePreparedDocument(doc config.Document) {
	source := doc.Sources["company"]
	source.Owners[0] = "Intruder"
	doc.Sources["company"] = config.Source{Owners: []string{"Replacement"}, Credential: "other"}
	doc.Sources["extra"] = source
	filter := doc.Filters["apps"]
	filter.Sources[0] = "missing"
	filter.Include[0].Topics.All[0] = "changed-all"
	filter.Include[0].Topics.Any[0] = "changed-any"
	filter.Include[0].Repositories[0] = "Acme/changed"
	filter.Include[0].NameRegex = "["
	filter.Include[1] = config.Rule{NameRegex: "changed"}
	filter.Exclude[0].Topics.All[0] = "changed-blocked"
	filter.Exclude[0].Topics.Any[0] = "changed-sandbox"
	filter.Exclude[0].Repositories[0] = "Acme/changed-exclusion"
	filter.Exclude[0].NameRegex = "["
	doc.Filters["apps"] = config.FilterSpec{}
	doc.Filters["extra"] = filter
}

func TestParseSnapshotsCompleteDocumentWithoutLosingCaseOrOrder(t *testing.T) {
	input := preparedDocument()
	cfg, err := config.Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsZero() {
		t.Fatal("successful Parse returned an uninitialized config")
	}
	mutatePreparedDocument(input)
	if got, want := cfg.Document(), preparedDocument(); !reflect.DeepEqual(got, want) {
		t.Fatalf("caller mutation changed prepared configuration or declaration order/case: got %#v, want %#v", got, want)
	}
	filter, ok := cfg.Filters()["apps"]
	if !ok || filter.IsZero() || !filter.Matches("Acme", "service-api", []string{"gitops", "managed", "worker"}) {
		t.Fatal("prepared predicate no longer matches original declaration")
	}
	if filter.Matches("Acme", "service-api", []string{"gitops", "managed", "worker", "blocked", "sandbox"}) {
		t.Fatal("prepared predicate lost exclusion after caller mutation")
	}
	if !filter.Matches("Acme", "legacy", nil) {
		t.Fatal("prepared predicate lost second include after caller mutation")
	}
}

func TestPreparedAccessorsReturnIndependentSnapshots(t *testing.T) {
	cfg, err := config.Parse(preparedDocument())
	if err != nil {
		t.Fatal(err)
	}
	mutatePreparedDocument(cfg.Document())
	sources := cfg.Sources()
	sources["company"].Owners[0] = "Intruder"
	delete(sources, "company")
	filters := cfg.Filters()
	filter := filters["apps"]
	filter.Sources()[0] = "missing"
	filter.Repositories()[0] = "Acme/changed"
	delete(filters, "apps")
	server, scan := cfg.Server(), cfg.Scan()
	server.Listen = "invalid"
	scan.Timeout = -1
	if got := cfg.Document(); !reflect.DeepEqual(got, preparedDocument()) {
		t.Fatalf("accessor mutated prepared configuration: %#v", got)
	}
	if !reflect.DeepEqual(cfg.Filters()["apps"].Sources(), []config.SourceName{"company"}) {
		t.Fatal("filter sources accessor exposed mutable state")
	}
	if got, want := cfg.Filters()["apps"].Repositories(), []string{"Acme/service-api", "Acme/service-worker", "Acme/legacy", "Acme/service-api"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("explicit repository diagnostics changed declaration order/case: got %#v want %#v", got, want)
	}
	if cfg.Server().Listen != ":8080" || cfg.Scan().Timeout != config.Duration(2*time.Minute) {
		t.Fatal("scalar accessors changed config")
	}
}

func TestParseRejectsInvalidProgrammaticDocumentAtConfigBoundary(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*config.Document)
		path   string
	}{
		{"zero version", func(doc *config.Document) { doc.Version = 0 }, "version"},
		{"empty listen", func(doc *config.Document) { doc.Server.Listen = "" }, "server.listen"},
		{"zero interval", func(doc *config.Document) { doc.Scan.Interval = 0 }, "scan.interval"},
		{"zero timeout", func(doc *config.Document) { doc.Scan.Timeout = 0 }, "scan.timeout"},
		{"missing source", func(doc *config.Document) { delete(doc.Sources, "company") }, "filters.apps.sources"},
		{"bad regexp", func(doc *config.Document) { doc.Filters["apps"].Include[0].NameRegex = "[" }, "filters.apps.include[0].nameRegex"},
		{"empty all", func(doc *config.Document) { doc.Filters["apps"].Include[0].Topics.All = []string{} }, "filters.apps.include[0].topics.all"},
		{"empty repository list", func(doc *config.Document) { doc.Filters["apps"].Include[0].Repositories = []string{} }, "filters.apps.include[0].repositories"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := preparedDocument()
			tc.mutate(&doc)
			cfg, err := config.Parse(doc)
			if err == nil || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("invalid declaration must fail in config with field path %q: %v", tc.path, err)
			}
			if !cfg.IsZero() {
				t.Fatal("failed Parse exposed a usable partial configuration")
			}
		})
	}
}

func TestRuntimePreparationFreezesBindingsAndPreservesDeclaredDocument(t *testing.T) {
	cfg, err := config.Parse(preparedDocument())
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"FRD_LISTEN": "127.0.0.1:9000", "FRD_SCAN_INTERVAL": "1m", "FRD_SCAN_TIMEOUT": "30s", "UNRELATED_SECRET": "must-not-be-retained"}
	names := []config.CredentialName{"company-app"}
	runtime, err := config.Prepare(cfg, env, names)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.IsZero() {
		t.Fatal("successful preparation returned zero runtime")
	}
	env["FRD_SCAN_INTERVAL"] = "invalid"
	names[0] = "other"
	runtime.Overrides()["FRD_LISTEN"] = "invalid"
	mutatePreparedDocument(runtime.Declared().Document())
	mutatePreparedDocument(runtime.Effective().Document())
	if got := runtime.Declared().Document(); !reflect.DeepEqual(got, preparedDocument()) {
		t.Fatalf("runtime changed declaration: %#v", got)
	}
	want := preparedDocument()
	want.Server.Listen = "127.0.0.1:9000"
	want.Scan.Interval = config.Duration(time.Minute)
	want.Scan.Timeout = config.Duration(30 * time.Second)
	if got := runtime.Effective().Document(); !reflect.DeepEqual(got, want) {
		t.Fatalf("runtime failed to preserve effective config: %#v", got)
	}
	wantOverrides := map[string]string{"FRD_LISTEN": "127.0.0.1:9000", "FRD_SCAN_INTERVAL": "1m", "FRD_SCAN_TIMEOUT": "30s"}
	if got := runtime.Overrides(); !reflect.DeepEqual(got, wantOverrides) {
		t.Fatalf("override snapshot leaked unrelated env or was mutable: %#v", got)
	}
	candidateDoc := preparedDocument()
	candidateDoc.Scan.Interval = config.Duration(5 * time.Minute)
	candidate, err := config.Parse(candidateDoc)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := runtime.PrepareCandidate(candidate)
	if err != nil {
		t.Fatal("caller mutation changed frozen credential names or environment:", err)
	}
	if preview.Declared().Scan().Interval != config.Duration(5*time.Minute) || preview.Effective().Scan().Interval != config.Duration(time.Minute) {
		t.Fatal("candidate declaration and runtime override were not kept separately")
	}
	if !reflect.DeepEqual(runtime.Declared().Document(), preparedDocument()) {
		t.Fatal("candidate preparation changed active config")
	}
}

func TestRuntimeRejectsUnknownCredentialBeforeExecutionAndKeepsOriginal(t *testing.T) {
	cfg, err := config.Parse(preparedDocument())
	if err != nil {
		t.Fatal(err)
	}
	if runtime, err := config.Prepare(cfg, nil, []config.CredentialName{"other"}); err == nil || !runtime.IsZero() || !strings.Contains(err.Error(), "sources.company.credential") {
		t.Fatalf("unresolved config escaped preparation: %#v %v", runtime, err)
	}
	runtime, err := config.Prepare(cfg, nil, []config.CredentialName{"company-app"})
	if err != nil {
		t.Fatal(err)
	}
	doc := preparedDocument()
	source := doc.Sources["company"]
	source.Credential = "new-app"
	doc.Sources["company"] = source
	candidate, err := config.Parse(doc)
	if err != nil {
		t.Fatal("credential names are resolved by runtime binding, not document parsing:", err)
	}
	if proposed, err := runtime.PrepareCandidate(candidate); err == nil || !proposed.IsZero() || !strings.Contains(err.Error(), "sources.company.credential") {
		t.Fatalf("unknown candidate credential accepted: %#v %v", proposed, err)
	}
	if !reflect.DeepEqual(runtime.Declared().Document(), preparedDocument()) {
		t.Fatal("failed candidate preparation changed active declaration")
	}
	if failed, err := config.Prepare(cfg, map[string]string{"FRD_SCAN_TIMEOUT": "0s"}, []config.CredentialName{"company-app"}); err == nil || !failed.IsZero() || !strings.Contains(err.Error(), "FRD_SCAN_TIMEOUT") {
		t.Fatalf("invalid runtime override accepted: %#v %v", failed, err)
	}
	if !reflect.DeepEqual(cfg.Document(), preparedDocument()) {
		t.Fatal("failed runtime preparation changed parsed config")
	}
}

func TestZeroPreparedValuesAreRejectedAtPreparationBoundaries(t *testing.T) {
	var cfg config.Config
	var runtime config.Runtime
	var filter config.Filter
	if !cfg.IsZero() || !runtime.IsZero() || !filter.IsZero() {
		t.Fatal("zero prepared values are reported as initialized")
	}
	if prepared, err := config.Prepare(cfg, nil, nil); err == nil || !prepared.IsZero() {
		t.Fatalf("zero config prepared successfully: %#v %v", prepared, err)
	}
	valid, err := config.Parse(preparedDocument())
	if err != nil {
		t.Fatal(err)
	}
	if prepared, err := runtime.PrepareCandidate(valid); err == nil || !prepared.IsZero() {
		t.Fatalf("zero runtime accepted candidate: %#v %v", prepared, err)
	}
	ready, err := config.Prepare(valid, nil, []config.CredentialName{"company-app"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared, err := ready.PrepareCandidate(cfg); err == nil || !prepared.IsZero() {
		t.Fatalf("zero candidate accepted: %#v %v", prepared, err)
	}
}

func TestPreparedConfigurationCanBeSharedWithIndependentSnapshots(t *testing.T) {
	cfg, err := config.Parse(preparedDocument())
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			for range 50 {
				mutatePreparedDocument(cfg.Document())
				if !cfg.Filters()["apps"].Matches("Acme", "legacy", nil) {
					t.Error("concurrent snapshot mutation changed prepared predicate")
					return
				}
			}
		})
	}
	workers.Wait()
	if !reflect.DeepEqual(cfg.Document(), preparedDocument()) {
		t.Fatal("concurrent snapshots changed configuration")
	}
}
