// Package service owns immutable discovery generations and isolated previews.
package service

import (
	"context"
	"encoding/json/jsontext"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
)

type Scanner interface {
	Scan(context.Context, map[config.SourceName]config.Source) map[config.SourceName]github.Result
}
type Options struct {
	Now                  func() time.Time
	MaxInputs            int
	MaxResponseBytes     int
	DryRunTimeout        time.Duration
	MaxConcurrentDryRuns int
}
type ErrorCode string
type Error struct {
	Code    ErrorCode           `json:"code"`
	Message string              `json:"message"`
	Sources []config.SourceName `json:"sources,omitempty"`
}

func (e *Error) Error() string { return string(e.Code) + ": " + e.Message }

type SourceStatus struct {
	Ready     bool      `json:"ready"`
	ErrorCode ErrorCode `json:"errorCode,omitempty"`
}
type FilterStatus struct {
	Ready     bool      `json:"ready"`
	Count     int       `json:"count"`
	ErrorCode ErrorCode `json:"errorCode,omitempty"`
}
type Status struct {
	Revision   string                             `json:"revision"`
	Generation uint64                             `json:"generation"`
	ScannedAt  time.Time                          `json:"scannedAt"`
	Sources    map[config.SourceName]SourceStatus `json:"sources"`
	Filters    map[config.FilterName]FilterStatus `json:"filters"`
}
type Operation string
type Change struct {
	Operation Operation      `json:"operation"`
	Path      string         `json:"path"`
	Before    jsontext.Value `json:"before,omitzero"`
	After     jsontext.Value `json:"after,omitzero"`
}
type ChangeKind string
type InputChange struct {
	Before discovery.Input `json:"before"`
	After  discovery.Input `json:"after"`
}
type FilterDiff struct {
	Kind                  ChangeKind        `json:"kind"`
	BeforeCount           int               `json:"beforeCount"`
	AfterCount            int               `json:"afterCount"`
	Added                 []discovery.Input `json:"added"`
	Removed               []discovery.Input `json:"removed"`
	Changed               []InputChange     `json:"changed"`
	Unchanged             int               `json:"unchanged"`
	Inputs                []discovery.Input `json:"inputs"`
	UnmatchedRepositories []string          `json:"unmatchedRepositories"`
}
type Drift struct {
	Available bool       `json:"available"`
	Diff      FilterDiff `json:"diff"`
}
type Report struct {
	Version           int                              `json:"version"`
	BaseRevision      string                           `json:"baseRevision"`
	CandidateRevision string                           `json:"candidateRevision"`
	BaseGeneration    uint64                           `json:"baseGeneration"`
	ObservedAt        time.Time                        `json:"observedAt"`
	HasChanges        bool                             `json:"hasChanges"`
	ConfigChanges     []Change                         `json:"configChanges"`
	EffectiveChanges  []Change                         `json:"effectiveChanges"`
	Overrides         map[string]string                `json:"overrides"`
	Filters           map[config.FilterName]FilterDiff `json:"filters"`
	Drift             map[config.FilterName]Drift      `json:"drift"`
}
