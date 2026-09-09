package service

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/jsonwire"
)

// DryRun compares the complete candidate against the loaded configuration using
// a fresh union of catalogs. It never publishes a generation or mutates options.
func (s *Service) DryRun(ctx context.Context, candidate config.Config) (Report, error) {
	base := s.published.Load()
	proposed, err := s.runtime.PrepareCandidate(candidate)
	if err != nil {
		return Report{}, invalidConfig(err)
	}
	effective := proposed.Effective()
	select {
	case s.previewGate <- struct{}{}:
		defer func() { <-s.previewGate }()
	default:
		return Report{}, &Error{Code: "busy", Message: "another dry-run is in progress"}
	}
	ctx, cancel := context.WithTimeout(ctx, s.options.DryRunTimeout)
	defer cancel()
	report := Report{Version: 1, BaseRevision: s.revision, CandidateRevision: configRevision(candidate), BaseGeneration: base.number, ConfigChanges: diffConfig(s.runtime.Declared(), candidate), EffectiveChanges: diffConfig(s.runtime.Effective(), effective), Overrides: proposed.Overrides()}
	union, beforeNames, afterNames := unionSources(s.runtime.Effective().Sources(), effective.Sources())
	fresh := s.scanner.Scan(ctx, union)
	if ctx.Err() != nil {
		return report, &Error{Code: "source_unavailable", Message: "dry-run scan was canceled or exceeded its deadline"}
	}
	for name := range union {
		result, exists := fresh[name]
		if !exists || result.Err != nil {
			return report, &Error{Code: "source_unavailable", Message: "dry-run could not obtain all required GitHub catalogs"}
		}
	}
	before, err := s.evaluateFilters(s.runtime.Effective().Filters(), remapCatalogs(fresh, beforeNames))
	if err != nil {
		return report, err
	}
	after, err := s.evaluateFilters(effective.Filters(), remapCatalogs(fresh, afterNames))
	if err != nil {
		return report, err
	}
	report.ObservedAt = s.options.Now()
	report.Filters = make(map[config.FilterName]FilterDiff)
	report.Drift = make(map[config.FilterName]Drift)
	report.HasChanges = len(report.ConfigChanges) != 0 || len(report.EffectiveChanges) != 0
	names := maps.Clone(before)
	maps.Copy(names, after)
	for _, name := range keys(names) {
		old, oldExists := before[name]
		proposed, newExists := after[name]
		diff := diffInputs(old.Inputs, proposed.Inputs)
		if !oldExists {
			diff.Kind = "added"
		} else if !newExists {
			diff.Kind = "removed"
		}
		diff.UnmatchedRepositories = append([]string{}, proposed.UnmatchedRepositories...)
		report.Filters[name] = diff
		if diff.Kind != "unchanged" {
			report.HasChanges = true
		}
		if !oldExists {
			continue
		}
		published, exists := base.filters[name]
		if base.number == 0 || !exists || published.err != nil {
			report.Drift[name] = Drift{Available: false}
			continue
		}
		drift := diffInputs(published.result.Inputs, old.Inputs)
		report.Drift[name] = Drift{Available: true, Diff: drift}
		if drift.Kind != "unchanged" {
			report.HasChanges = true
		}
	}
	return report, nil
}

func (s *Service) evaluateFilters(filters map[config.FilterName]config.Filter, catalogs map[config.SourceName][]discovery.Repository) (map[config.FilterName]discovery.Result, error) {
	results := make(map[config.FilterName]discovery.Result, len(filters))
	for _, name := range keys(filters) {
		result, err := discovery.Evaluate(filters[name], catalogs)
		if err != nil {
			return nil, &Error{Code: "source_unavailable", Message: "dry-run catalog failed integrity validation"}
		}
		if err := s.checkResult(result.Inputs); err != nil {
			return nil, err
		}
		results[name] = result
	}
	return results, nil
}

func unionSources(before, after map[config.SourceName]config.Source) (map[config.SourceName]config.Source, map[config.SourceName]config.SourceName, map[config.SourceName]config.SourceName) {
	union := make(map[config.SourceName]config.Source)
	identities := make(map[string]config.SourceName)
	reserved := make(map[config.SourceName]bool)
	for name := range before {
		reserved[name] = true
	}
	for name := range after {
		reserved[name] = true
	}
	mapping := func(sources map[config.SourceName]config.Source) map[config.SourceName]config.SourceName {
		result := make(map[config.SourceName]config.SourceName)
		for _, name := range keys(sources) {
			source := sources[name]
			source.Owners = slices.Clone(source.Owners)
			for i := range source.Owners {
				source.Owners[i] = strings.ToLower(source.Owners[i])
			}
			slices.Sort(source.Owners)
			source.Owners = slices.Compact(source.Owners)
			encoded, _ := jsonwire.Marshal(source)
			identity := string(encoded)
			scope, exists := identities[identity]
			if !exists {
				scope = name
				if _, occupied := union[scope]; occupied {
					for i := len(union); ; i++ {
						scope = config.SourceName(fmt.Sprintf("preview-%d", i))
						_, used := union[scope]
						if !reserved[scope] && !used {
							break
						}
					}
				}
				identities[identity] = scope
				union[scope] = source
			}
			result[name] = scope
		}
		return result
	}
	oldNames := mapping(before)
	newNames := mapping(after)
	return union, oldNames, newNames
}

func remapCatalogs(fresh map[config.SourceName]github.Result, mapping map[config.SourceName]config.SourceName) map[config.SourceName][]discovery.Repository {
	result := make(map[config.SourceName][]discovery.Repository, len(mapping))
	for name, scope := range mapping {
		result[name] = fresh[scope].Repositories
	}
	return result
}
