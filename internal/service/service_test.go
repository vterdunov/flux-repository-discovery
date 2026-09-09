package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

type scannerFunc func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result

func (f scannerFunc) Scan(ctx context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
	return f(ctx, sources)
}

func baseConfig() config.Document {
	return config.Document{Version: 1, Server: config.Server{Listen: ":8080"}, Scan: config.Scan{Interval: config.Duration(10 * time.Minute), Timeout: config.Duration(2 * time.Minute)}, Sources: map[config.SourceName]config.Source{"company": {Owners: []string{"acme"}, Credential: "token"}}, Filters: map[config.FilterName]config.FilterSpec{"apps": {Sources: []config.SourceName{"company"}, Include: []config.Rule{{Topics: &config.Topics{All: []string{"gitops"}}}}}}}
}
func repository(id discovery.RepositoryID, name string, topics ...string) discovery.Repository {
	return discovery.Repository{ID: id, Owner: "acme", Name: name, Topics: topics}
}
func mustConfig(t *testing.T, document config.Document) config.Config {
	t.Helper()
	parsed, err := config.Parse(document)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
func mustService(t *testing.T, document config.Document, scanner service.Scanner, opts service.Options, environments ...map[string]string) *service.Service {
	t.Helper()
	var environment map[string]string
	if len(environments) > 0 {
		environment = environments[0]
	}
	runtime, err := config.Prepare(mustConfig(t, document), environment, []config.CredentialName{"token", "token2"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := service.New(runtime, scanner, opts)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func requireCode(t *testing.T, err error, code service.ErrorCode) {
	t.Helper()
	var typed *service.Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

func TestGenerationNeverFallsBackAfterFailureAndRecoversToEmpty(t *testing.T) {
	var current = github.Result{Repositories: []discovery.Repository{repository(1, "api", "gitops")}}
	s := mustService(t, baseConfig(), scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		return map[config.SourceName]github.Result{"company": current}
	}), service.Options{})
	_, err := s.Inputs("apps")
	requireCode(t, err, "not_ready")
	_, err = s.Inputs("missing")
	requireCode(t, err, "not_found")
	if err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := s.Inputs("apps")
	if err != nil || len(got) != 1 {
		t.Fatalf("first successful generation: %#v %v", got, err)
	}
	first := s.Status()
	if first.Generation == 0 || first.Revision == "" || first.ScannedAt.IsZero() {
		t.Fatalf("missing generation metadata: %#v", first)
	}
	current = github.Result{Repositories: []discovery.Repository{repository(2, "partial", "gitops")}, Err: errors.New("upstream token=super-secret")}
	if err = s.Scan(context.Background()); err == nil {
		t.Fatal("scan failure hidden")
	}
	got, err = s.Inputs("apps")
	requireCode(t, err, "source_unavailable")
	if len(got) != 0 {
		t.Fatalf("partial or stale success leaked: %#v", got)
	}
	encoded, _ := json.Marshal(s.Status())
	if strings.Contains(string(encoded), "super-secret") || strings.Contains(err.Error(), "super-secret") {
		t.Fatal("upstream secret leaked")
	}
	if s.Status().Generation <= first.Generation || s.Status().Filters["apps"].Ready {
		t.Fatal("failed attempt was not published")
	}
	current = github.Result{Repositories: []discovery.Repository{repository(1, "api", "unmanaged")}}
	if err = s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err = s.Inputs("apps")
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("successful empty generation: %#v %v", got, err)
	}
}

func TestFailedSourceOnlyDisablesDependentFilters(t *testing.T) {
	cfg := baseConfig()
	cfg.Sources["personal"] = config.Source{Owners: []string{"bob"}, Credential: "token2"}
	cfg.Filters["personal"] = config.FilterSpec{Sources: []config.SourceName{"personal"}, Include: []config.Rule{{NameRegex: ".*"}}}
	cfg.Filters["combined"] = config.FilterSpec{Sources: []config.SourceName{"company", "personal"}, Include: []config.Rule{{NameRegex: ".*"}}}
	s := mustService(t, cfg, scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		return map[config.SourceName]github.Result{"company": {Err: errors.New("failed")}, "personal": {Repositories: []discovery.Repository{{ID: 2, Owner: "bob", Name: "repo"}}}}
	}), service.Options{})
	if err := s.Scan(context.Background()); err == nil {
		t.Fatal("scan should report source failure")
	}
	for _, name := range []config.FilterName{"apps", "combined"} {
		_, err := s.Inputs(name)
		requireCode(t, err, "source_unavailable")
	}
	got, err := s.Inputs("personal")
	if err != nil || len(got) != 1 || got[0].FullName != "bob/repo" {
		t.Fatalf("independent filter unavailable: %#v %v", got, err)
	}
	if !s.Status().Sources["personal"].Ready || s.Status().Sources["company"].Ready {
		t.Fatalf("source status incorrect: %#v", s.Status())
	}
}

func TestMissingScannerResultIsFailureAndInputsAreCopies(t *testing.T) {
	results := map[config.SourceName]github.Result{"company": {Repositories: []discovery.Repository{repository(1, "api", "gitops")}}}
	s := mustService(t, baseConfig(), scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		return results
	}), service.Options{})
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := s.Inputs("apps")
	if err != nil {
		t.Fatal(err)
	}
	first[0].Name = "tampered"
	again, err := s.Inputs("apps")
	if err != nil || again[0].Name != "api" {
		t.Fatal("caller modified published generation")
	}
	results = map[config.SourceName]github.Result{}
	if err := s.Scan(context.Background()); err == nil {
		t.Fatal("missing scanner entry accepted")
	}
	_, err = s.Inputs("apps")
	requireCode(t, err, "source_unavailable")
}

func TestStalenessAndResponseLimitsFailClosed(t *testing.T) {
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	scan := scannerFunc(func(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		return map[config.SourceName]github.Result{"company": {Repositories: []discovery.Repository{repository(1, "api", "gitops"), repository(2, "worker", "gitops")}}}
	})
	s := mustService(t, baseConfig(), scan, service.Options{Now: func() time.Time { return now }})
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(12*time.Minute + time.Nanosecond)
	_, err := s.Inputs("apps")
	requireCode(t, err, "stale")
	for _, opts := range []service.Options{{MaxInputs: 1}, {MaxResponseBytes: 20}} {
		limited := mustService(t, baseConfig(), scan, opts)
		_ = limited.Scan(context.Background())
		items, err := limited.Inputs("apps")
		requireCode(t, err, "result_limit")
		if len(items) != 0 {
			t.Fatal("limit violation truncated result")
		}
	}
}

func TestDryRunUsesFreshSharedCatalogsAndSeparatesDrift(t *testing.T) {
	cfg := baseConfig()
	cfg.Filters["legacy"] = config.FilterSpec{Sources: []config.SourceName{"company"}, Include: []config.Rule{{NameRegex: "^legacy$"}}}
	phase := 0
	var previewScopes []config.Source
	scan := scannerFunc(func(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		out := make(map[config.SourceName]github.Result)
		for name, source := range sources {
			if phase == 0 {
				out[name] = github.Result{Repositories: []discovery.Repository{repository(1, "old-name", "gitops"), repository(2, "legacy", "gitops")}}
				continue
			}
			previewScopes = append(previewScopes, source)
			if source.Owners[0] == "bob" {
				out[name] = github.Result{Repositories: []discovery.Repository{{ID: 20, Owner: "bob", Name: "tools"}}}
				continue
			}
			out[name] = github.Result{Repositories: []discovery.Repository{repository(1, "new-name", "gitops", "managed"), repository(2, "legacy"), repository(3, "unmanaged", "gitops"), repository(4, "managed", "gitops", "managed")}}
		}
		return out
	})
	s := mustService(t, cfg, scan, service.Options{}, map[string]string{"FRD_SCAN_INTERVAL": "1m"})
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	published, err := s.Inputs("apps")
	if err != nil {
		t.Fatal(err)
	}
	statusBefore := s.Status()
	phase = 1
	candidate := baseConfig()
	candidate.Scan.Interval = config.Duration(5 * time.Minute)
	candidate.Server.Listen = ":9090"
	candidate.Sources["company"] = config.Source{Owners: []string{"acme"}, Credential: "token2"}
	candidate.Sources["personal"] = config.Source{Owners: []string{"bob"}, Credential: "token2"}
	candidate.Filters["apps"] = config.FilterSpec{Sources: []config.SourceName{"company"}, Include: []config.Rule{{Topics: &config.Topics{All: []string{"gitops", "managed"}}}, {Repositories: []string{"acme/missing"}}}}
	candidate.Filters["tools"] = config.FilterSpec{Sources: []config.SourceName{"personal"}, Include: []config.Rule{{NameRegex: ".*"}}}
	report, err := s.DryRun(context.Background(), mustConfig(t, candidate))
	if err != nil {
		t.Fatal(err)
	}
	if report.Version != 1 || !report.HasChanges || report.BaseRevision != statusBefore.Revision || report.CandidateRevision == report.BaseRevision || report.BaseGeneration != statusBefore.Generation || report.ObservedAt.IsZero() {
		t.Fatalf("report metadata missing: %#v", report)
	}
	if len(previewScopes) != 3 {
		t.Fatalf("fresh union must scan 3 distinct owner+credential scopes once each: %#v", previewScopes)
	}
	seen := map[string]int{}
	for _, scope := range previewScopes {
		seen[scope.Owners[0]+"/"+string(scope.Credential)]++
	}
	if !reflect.DeepEqual(seen, map[string]int{"acme/token": 1, "acme/token2": 1, "bob/token2": 1}) {
		t.Fatalf("scopes=%#v", seen)
	}
	apps := report.Filters["apps"]
	if apps.BeforeCount != 3 || apps.AfterCount != 2 || apps.Unchanged != 2 || len(apps.Removed) != 1 || apps.Removed[0].ID != "3" || len(apps.Added) != 0 || len(apps.Changed) != 0 {
		t.Fatalf("configuration result diff mixed in drift: %#v", apps)
	}
	if len(apps.Inputs) != 2 || apps.Inputs[0].FullName != "acme/new-name" || !reflect.DeepEqual(apps.UnmatchedRepositories, []string{"acme/missing"}) {
		t.Fatalf("proposed output or diagnostics missing: %#v", apps)
	}
	if report.Filters["legacy"].Kind != "removed" || report.Filters["tools"].Kind != "added" || report.Filters["tools"].AfterCount != 1 {
		t.Fatalf("endpoint additions/removals missing: %#v", report.Filters)
	}
	drift := report.Drift["apps"]
	if !drift.Available || len(drift.Diff.Changed) != 1 || drift.Diff.Changed[0].Before.Name != "old-name" || drift.Diff.Changed[0].After.Name != "new-name" || len(drift.Diff.Removed) != 1 || len(drift.Diff.Added) != 2 {
		t.Fatalf("separate drift incorrect: %#v", drift)
	}
	for _, path := range []string{"scan.interval", "server.listen", "sources.company.credential", "sources.personal", "filters.apps.include", "filters.legacy", "filters.tools"} {
		if !hasPath(report.ConfigChanges, path) {
			t.Errorf("config diff missing %s: %#v", path, report.ConfigChanges)
		}
	}
	if hasPath(report.EffectiveChanges, "scan.interval") || report.Overrides["FRD_SCAN_INTERVAL"] != "1m" {
		t.Fatalf("server env masking not explained: %#v", report)
	}
	if !hasPath(report.EffectiveChanges, "server.listen") {
		t.Fatal("unmasked effective changes missing")
	}
	after, err := s.Inputs("apps")
	if err != nil || !reflect.DeepEqual(after, published) || !reflect.DeepEqual(s.Status(), statusBefore) {
		t.Fatal("dry-run mutated active configuration or generation")
	}
}
func hasPath(changes []service.Change, path string) bool {
	for _, change := range changes {
		if change.Path == path || strings.HasPrefix(change.Path, path+"[") || strings.HasPrefix(change.Path, path+".") {
			return true
		}
	}
	return false
}

func TestDryRunWithoutGenerationAndValidationBeforeNetwork(t *testing.T) {
	calls := 0
	scan := scannerFunc(func(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		calls++
		out := map[config.SourceName]github.Result{}
		for name := range sources {
			out[name] = github.Result{Repositories: []discovery.Repository{}}
		}
		return out
	})
	s := mustService(t, baseConfig(), scan, service.Options{})
	report, err := s.DryRun(context.Background(), mustConfig(t, baseConfig()))
	if err != nil {
		t.Fatal(err)
	}
	if report.Drift["apps"].Available || report.BaseGeneration != 0 || calls != 1 {
		t.Fatalf("unavailable drift or redundant scans: %#v calls=%d", report, calls)
	}
	_, err = s.Inputs("apps")
	requireCode(t, err, "not_ready")
	candidate := baseConfig()
	candidate.Sources["company"] = config.Source{Owners: []string{"acme"}, Credential: "unregistered"}
	_, err = s.DryRun(context.Background(), mustConfig(t, candidate))
	requireCode(t, err, "invalid_config")
	if calls != 1 {
		t.Fatal("invalid credential triggered network")
	}
	candidate = baseConfig()
	candidate.Filters["apps"] = config.FilterSpec{Sources: []config.SourceName{"missing"}, Include: []config.Rule{{NameRegex: ".*"}}}
	_, err = config.Parse(candidate)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("unknown source must fail during config parsing: %v", err)
	}
	if calls != 1 {
		t.Fatal("invalid filter triggered network")
	}
}

func TestDryRunFailureDoesNotPublishPartialDiffOrChangeState(t *testing.T) {
	fail := false
	scan := scannerFunc(func(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		out := map[config.SourceName]github.Result{}
		for name := range sources {
			if fail {
				out[name] = github.Result{Err: errors.New("super-secret")}
			} else {
				out[name] = github.Result{Repositories: []discovery.Repository{repository(1, "api", "gitops")}}
			}
		}
		return out
	})
	s := mustService(t, baseConfig(), scan, service.Options{})
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := s.Status()
	fail = true
	report, err := s.DryRun(context.Background(), mustConfig(t, baseConfig()))
	requireCode(t, err, "source_unavailable")
	if len(report.Filters) != 0 || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("partial report or secret: %#v %v", report, err)
	}
	items, err := s.Inputs("apps")
	if err != nil || len(items) != 1 || !reflect.DeepEqual(s.Status(), before) {
		t.Fatal("failed dry-run affected active output")
	}
}

func TestDryRunLimitCancellationAndRuntimeDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan time.Time, 2)
		scan := scannerFunc(func(ctx context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
			deadline, _ := ctx.Deadline()
			entered <- deadline
			<-ctx.Done()
			out := map[config.SourceName]github.Result{}
			for name := range sources {
				out[name] = github.Result{Err: ctx.Err()}
			}
			return out
		})
		s := mustService(t, baseConfig(), scan, service.Options{DryRunTimeout: 5 * time.Second})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		candidate := baseConfig()
		candidate.Scan.Timeout = config.Duration(time.Hour)
		parsedCandidate := mustConfig(t, candidate)
		go func() { _, err := s.DryRun(ctx, parsedCandidate); done <- err }()
		var deadline time.Time
		select {
		case deadline = <-entered:
		case err := <-done:
			t.Fatalf("dry-run exited before scanning: %v", err)
		}
		if !deadline.Equal(time.Now().Add(5 * time.Second)) {
			t.Fatalf("candidate changed runtime deadline: %v", deadline)
		}
		_, err := s.DryRun(context.Background(), parsedCandidate)
		requireCode(t, err, "busy")
		cancel()
		if err := <-done; err == nil {
			t.Fatal("cancellation reported success")
		}
		go func() { _, err := s.DryRun(context.Background(), parsedCandidate); done <- err }()
		select {
		case <-entered:
		case err := <-done:
			t.Fatalf("dry-run exited before scanning: %v", err)
		}
		time.Sleep(5 * time.Second)
		if err := <-done; err == nil {
			t.Fatal("deadline reported success")
		}
	})
}

func TestRunImmediateScanIntervalAfterCompletionAndGracefulStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := baseConfig()
		cfg.Scan.Interval = config.Duration(3 * time.Second)
		cfg.Scan.Timeout = config.Duration(10 * time.Second)
		started := make(chan time.Time, 4)
		var active atomic.Int32
		s := mustService(t, cfg, scannerFunc(func(ctx context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
			if active.Add(1) != 1 {
				t.Error("overlapping scan cycles")
			}
			defer active.Add(-1)
			started <- time.Now()
			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
			}
			return map[config.SourceName]github.Result{"company": {Repositories: []discovery.Repository{repository(1, "api", "gitops")}}}
		}), service.Options{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		start := time.Now()
		go func() { done <- s.Run(ctx) }()
		var when time.Time
		select {
		case when = <-started:
		case err := <-done:
			t.Fatalf("Run exited before initial scan: %v", err)
		}
		if !when.Equal(start) {
			t.Fatalf("initial scan delayed: %v", when.Sub(start))
		}
		if !s.Ready() {
			t.Fatal("running service not ready")
		}
		select {
		case when = <-started:
		case err := <-done:
			t.Fatalf("Run exited before next scan: %v", err)
		}
		if when.Sub(start) != 5*time.Second {
			t.Fatalf("next scan should start completion+interval, got %s", when.Sub(start))
		}
		cancel()
		err := <-done
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if s.Ready() {
			t.Fatal("stopped service remains ready")
		}
	})
}

func TestRunScanTimeoutCancelsWholeCycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := baseConfig()
		cfg.Scan.Timeout = config.Duration(time.Second)
		calls := make(chan context.Context, 1)
		s := mustService(t, cfg, scannerFunc(func(ctx context.Context, _ map[config.SourceName]config.Source) map[config.SourceName]github.Result {
			calls <- ctx
			<-ctx.Done()
			return map[config.SourceName]github.Result{"company": {Err: ctx.Err()}}
		}), service.Options{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- s.Run(ctx) }()
		var scanCtx context.Context
		select {
		case scanCtx = <-calls:
		case err := <-done:
			t.Fatalf("Run exited before scan: %v", err)
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if !errors.Is(scanCtx.Err(), context.DeadlineExceeded) {
			t.Fatalf("scan deadline missing: %v", scanCtx.Err())
		}
		_, err := s.Inputs("apps")
		requireCode(t, err, "source_unavailable")
		cancel()
		<-done
	})
}

func TestConstructorFreezesConfigAndRejectsUnknownCredentials(t *testing.T) {
	cfg := baseConfig()
	cfg.Sources["company"] = config.Source{Owners: []string{"acme"}, Credential: "unknown"}
	_, err := config.Prepare(mustConfig(t, cfg), nil, []config.CredentialName{"token"})
	if err == nil {
		t.Fatal("unknown startup credentials accepted")
	}
	cfg = baseConfig()
	env := map[string]string{"FRD_LISTEN": ":9000"}
	s := mustService(t, cfg, scannerFunc(func(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
		if sources["company"].Owners[0] != "acme" {
			t.Errorf("constructor retained caller config: %#v", sources)
		}
		return map[config.SourceName]github.Result{"company": {Repositories: []discovery.Repository{}}}
	}), service.Options{}, env)
	revision := s.Status().Revision
	cfg.Sources["company"].Owners[0] = "mutated"
	delete(cfg.Filters, "apps")
	env["FRD_LISTEN"] = ":9999"
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Status().Revision != revision {
		t.Fatal("external config mutation changed revision")
	}
	report, err := s.DryRun(context.Background(), mustConfig(t, baseConfig()))
	if err != nil {
		t.Fatal(err)
	}
	if report.Overrides["FRD_LISTEN"] != ":9000" {
		t.Fatalf("external env map mutation changed active overrides: %#v", report.Overrides)
	}
}

func TestPublishedGenerationRemainsWholeWhileNextScanRuns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		s := mustService(t, baseConfig(), scannerFunc(func(_ context.Context, _ map[config.SourceName]config.Source) map[config.SourceName]github.Result {
			calls++
			if calls == 2 {
				entered <- struct{}{}
				<-release
			}
			items := make([]discovery.Repository, 50)
			for i := range items {
				items[i] = repository(discovery.RepositoryID(i+1), fmt.Sprintf("generation-%d-%d", calls, i), "gitops")
			}
			return map[config.SourceName]github.Result{"company": {Repositories: items}}
		}), service.Options{})
		if err := s.Scan(context.Background()); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- s.Scan(context.Background()) }()
		<-entered
		for range 10 {
			items, err := s.Inputs("apps")
			if err != nil || len(items) != 50 {
				t.Fatalf("in-progress scan disrupted output: %v", err)
			}
			for _, item := range items {
				if !strings.HasPrefix(item.Name, "generation-1-") {
					t.Fatal("partially published new generation")
				}
			}
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		items, err := s.Inputs("apps")
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range items {
			if !strings.HasPrefix(item.Name, "generation-2-") {
				t.Fatal("mixed completed generation")
			}
		}
	})
}
