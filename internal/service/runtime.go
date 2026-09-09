package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"sync/atomic"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/jsonwire"
)

const (
	FluxMaxInputs        = 10000
	FluxMaxResponseBytes = 900 * 1024
)

type filterState struct {
	result discovery.Result
	err    *Error
}

type generation struct {
	number    uint64
	scannedAt time.Time
	sources   map[config.SourceName]SourceStatus
	filters   map[config.FilterName]filterState
}

// Service holds immutable configuration and atomically published generations.
// The Scanner must honor context cancellation and return complete source results.
type Service struct {
	runtime     config.Runtime
	scanner     Scanner
	options     Options
	revision    string
	scanGate    chan struct{}
	previewGate chan struct{}
	published   atomic.Pointer[generation]
	running     atomic.Bool
}

func New(runtime config.Runtime, scanner Scanner, options Options) (*Service, error) {
	if scanner == nil {
		return nil, errors.New("scanner is required")
	}
	if runtime.IsZero() {
		return nil, invalidConfig(errors.New("runtime configuration is not initialized"))
	}
	effective := runtime.Effective()
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MaxInputs == 0 {
		options.MaxInputs = FluxMaxInputs
	}
	if options.MaxResponseBytes == 0 {
		options.MaxResponseBytes = FluxMaxResponseBytes
	}
	if options.DryRunTimeout == 0 {
		options.DryRunTimeout = time.Duration(effective.Scan().Timeout)
	}
	if options.MaxConcurrentDryRuns == 0 {
		options.MaxConcurrentDryRuns = 1
	}
	if options.MaxInputs < 1 || options.MaxInputs > FluxMaxInputs || options.MaxResponseBytes < 1 || options.MaxResponseBytes > FluxMaxResponseBytes || options.DryRunTimeout < 0 || options.MaxConcurrentDryRuns < 1 {
		return nil, errors.New("invalid service limits")
	}
	s := &Service{runtime: runtime, scanner: scanner, options: options, revision: configRevision(runtime.Declared()), scanGate: make(chan struct{}, 1), previewGate: make(chan struct{}, options.MaxConcurrentDryRuns)}
	initial := &generation{sources: make(map[config.SourceName]SourceStatus), filters: make(map[config.FilterName]filterState)}
	for name := range effective.Sources() {
		initial.sources[name] = SourceStatus{ErrorCode: "not_ready"}
	}
	for name := range effective.Filters() {
		initial.filters[name] = filterState{err: &Error{Code: "not_ready", Message: "initial scan has not completed"}}
	}
	s.published.Store(initial)
	return s, nil
}

func configRevision(value config.Config) string {
	// The prepared config exposes the same canonical document used by the CLI.
	data, _ := jsonwire.Marshal(value.Document())
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func invalidConfig(err error) *Error { return &Error{Code: "invalid_config", Message: err.Error()} }

// Scan publishes successes and failures from one complete scan attempt. It
// serializes concurrent calls without preventing HTTP reads of the last attempt.
func (s *Service) Scan(ctx context.Context) error {
	select {
	case s.scanGate <- struct{}{}:
		defer func() { <-s.scanGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	effective := s.runtime.Effective()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(effective.Scan().Timeout))
	defer cancel()
	started := s.options.Now()
	results := s.scanner.Scan(ctx, effective.Sources())
	next, scanErr := s.buildGeneration(results)
	next.number = s.published.Load().number + 1
	next.scannedAt = s.options.Now()
	s.published.Store(next)
	logGeneration(ctx, next, results, next.scannedAt.Sub(started))
	if scanErr != nil {
		slog.Warn("discovery scan failed", "generation", next.number, "duration", next.scannedAt.Sub(started), "error", scanErr)
	} else {
		slog.Info("discovery scan completed", "generation", next.number, "duration", next.scannedAt.Sub(started), "sources", len(next.sources), "filters", len(next.filters))
	}
	return scanErr
}

func (s *Service) buildGeneration(results map[config.SourceName]github.Result) (*generation, error) {
	effective := s.runtime.Effective()
	sources, filters := effective.Sources(), effective.Filters()
	next := &generation{sources: make(map[config.SourceName]SourceStatus), filters: make(map[config.FilterName]filterState)}
	catalogs := make(map[config.SourceName][]discovery.Repository)
	failed := make([]config.SourceName, 0)
	for _, name := range keys(sources) {
		result, exists := results[name]
		if !exists || result.Err != nil {
			next.sources[name] = SourceStatus{ErrorCode: "source_unavailable"}
			failed = append(failed, name)
			continue
		}
		next.sources[name] = SourceStatus{Ready: true}
		catalogs[name] = result.Repositories
	}
	var scanErr error
	if len(failed) != 0 {
		scanErr = &Error{Code: "source_unavailable", Message: "one or more GitHub sources failed", Sources: failed}
	}
	for _, name := range keys(filters) {
		filter := filters[name]
		unavailable := make([]config.SourceName, 0)
		for _, source := range filter.Sources() {
			if !next.sources[source].Ready {
				unavailable = append(unavailable, source)
			}
		}
		if len(unavailable) != 0 {
			next.filters[name] = filterState{err: &Error{Code: "source_unavailable", Message: "required GitHub source is unavailable", Sources: uniqueNames(unavailable)}}
			continue
		}
		result, err := discovery.Evaluate(filter, catalogs)
		if err != nil {
			problem := &Error{Code: "source_unavailable", Message: "repository catalog failed integrity validation", Sources: filter.Sources()}
			next.filters[name] = filterState{err: problem}
			scanErr = problem
			continue
		}
		if err := s.checkResult(result.Inputs); err != nil {
			next.filters[name] = filterState{err: err}
			scanErr = err
			continue
		}
		next.filters[name] = filterState{result: result}
	}
	return next, scanErr
}

func (s *Service) checkResult(inputs []discovery.Input) *Error {
	if len(inputs) > s.options.MaxInputs {
		return &Error{Code: "result_limit", Message: "result exceeds input count limit"}
	}
	data, err := jsonwire.Marshal(struct {
		Inputs []discovery.Input `json:"inputs"`
	}{inputs})
	if err != nil || len(data) > s.options.MaxResponseBytes {
		return &Error{Code: "result_limit", Message: "result exceeds response size limit"}
	}
	return nil
}

// Run scans immediately, then waits one interval after each completed attempt.
func (s *Service) Run(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.running.CompareAndSwap(false, true) {
		return errors.New("service is already running")
	}
	defer s.running.Store(false)
	for {
		_ = s.Scan(ctx) // The complete attempt, including failure, is published and logged.
		if err := ctx.Err(); err != nil {
			return err
		}
		timer := time.NewTimer(time.Duration(s.runtime.Effective().Scan().Interval))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Service) Ready() bool { return s.running.Load() }

func (s *Service) isStale(current *generation) bool {
	scan := s.runtime.Effective().Scan()
	return current.number > 0 && s.options.Now().Sub(current.scannedAt) > time.Duration(scan.Interval)+time.Duration(scan.Timeout)
}

func (s *Service) Inputs(name config.FilterName) ([]discovery.Input, error) {
	current := s.published.Load()
	state, exists := current.filters[name]
	if !exists {
		return nil, &Error{Code: "not_found", Message: "filter does not exist"}
	}
	if state.err != nil {
		problem := *state.err
		problem.Sources = slices.Clone(state.err.Sources)
		return nil, &problem
	}
	if s.isStale(current) {
		return nil, &Error{Code: "stale", Message: "scheduled scan did not complete within its deadline"}
	}
	return slices.Clone(state.result.Inputs), nil
}

func (s *Service) Status() Status {
	current := s.published.Load()
	status := Status{Revision: s.revision, Generation: current.number, ScannedAt: current.scannedAt, Sources: maps.Clone(current.sources), Filters: make(map[config.FilterName]FilterStatus)}
	stale := s.isStale(current)
	for name, state := range current.filters {
		if state.err != nil {
			status.Filters[name] = FilterStatus{ErrorCode: state.err.Code}
		} else if stale {
			status.Filters[name] = FilterStatus{ErrorCode: "stale"}
		} else {
			status.Filters[name] = FilterStatus{Ready: true, Count: len(state.result.Inputs)}
		}
	}
	if stale {
		for name, state := range status.Sources {
			if state.Ready {
				status.Sources[name] = SourceStatus{ErrorCode: "stale"}
			}
		}
	}
	return status
}

func uniqueNames(names []config.SourceName) []config.SourceName {
	slices.Sort(names)
	return slices.Compact(names)
}

func keys[K ~string, V any](values map[K]V) []K {
	result := make([]K, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	slices.Sort(result)
	return result
}
