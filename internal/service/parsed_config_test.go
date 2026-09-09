package service_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

func TestParsedRuntimeZeroCannotStartService(t *testing.T) {
	calls := 0
	s, err := service.New(config.Runtime{}, scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		calls++
		return nil
	}), service.Options{})
	if err == nil || s != nil {
		t.Fatalf("zero runtime constructed service: service=%v error=%v", s, err)
	}
	if calls != 0 {
		t.Fatalf("zero runtime triggered %d scans", calls)
	}
}

func TestParsedConfigZeroCannotPreviewOrChangePublishedState(t *testing.T) {
	calls := 0
	s := mustService(t, baseConfig(), scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		calls++
		return map[config.SourceName]github.Result{"company": {Repositories: []discovery.Repository{repository(1, "api", "gitops")}}}
	}), service.Options{})
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := s.Status()
	report, err := s.DryRun(context.Background(), config.Config{})
	requireCode(t, err, "invalid_config")
	if !reflect.DeepEqual(report, service.Report{}) || calls != 1 {
		t.Fatalf("zero candidate reached scanner or produced report: calls=%d report=%#v", calls, report)
	}
	items, err := s.Inputs("apps")
	if err != nil || len(items) != 1 || items[0].FullName != "acme/api" || !reflect.DeepEqual(before, s.Status()) {
		t.Fatalf("zero candidate changed publication: inputs=%#v error=%v", items, err)
	}
}

func TestParsedConfigurationCopiesCannotAlterServicePublication(t *testing.T) {
	document := baseConfig()
	parsed := mustConfig(t, document)
	environment := map[string]string{"FRD_LISTEN": ":9000"}
	credentials := []config.CredentialName{"token"}
	runtime, err := config.Prepare(parsed, environment, credentials)
	if err != nil {
		t.Fatal(err)
	}
	scan := scannerFunc(func(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		if !reflect.DeepEqual(sources, map[config.SourceName]config.Source{"company": {Owners: []string{"acme"}, Credential: "token"}}) {
			t.Errorf("source changed through external aliases: %#v", sources)
		}
		return map[config.SourceName]github.Result{"company": {Repositories: []discovery.Repository{repository(1, "api", "gitops"), repository(2, "unmanaged")}}}
	})
	s, err := service.New(runtime, scan, service.Options{})
	if err != nil {
		t.Fatal(err)
	}
	revision := s.Status().Revision
	mutateDocument := func(copy config.Document) {
		copy.Sources["company"].Owners[0] = "changed"
		copy.Filters["apps"].Include[0].Topics.All[0] = "changed"
		delete(copy.Filters, "apps")
	}
	mutateDocument(document)
	mutateDocument(parsed.Document())
	mutateDocument(runtime.Declared().Document())
	mutateDocument(runtime.Effective().Document())
	parsed.Sources()["company"].Owners[0] = "changed"
	filters := parsed.Filters()
	filters["apps"].Sources()[0] = "missing"
	delete(filters, "apps")
	environment["FRD_LISTEN"] = ":9999"
	credentials[0] = "missing"
	runtime.Overrides()["FRD_LISTEN"] = ":9999"
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	inputs, err := s.Inputs("apps")
	want := []discovery.Input{{ID: "1", FullName: "acme/api", Owner: "acme", Name: "api"}}
	if err != nil || !reflect.DeepEqual(inputs, want) || s.Status().Revision != revision {
		t.Fatalf("external aliases changed publication: inputs=%#v error=%v revision=%s", inputs, err, s.Status().Revision)
	}
	before := s.Status()
	report, err := s.DryRun(context.Background(), mustConfig(t, baseConfig()))
	if err != nil {
		t.Fatal(err)
	}
	if report.HasChanges || len(report.ConfigChanges) != 0 || len(report.EffectiveChanges) != 0 || report.Overrides["FRD_LISTEN"] != ":9000" || !reflect.DeepEqual(before, s.Status()) {
		t.Fatalf("aliases changed runtime environment, credential registry, or configuration: %#v", report)
	}
}
