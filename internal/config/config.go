// Package config defines the user-facing discovery configuration.
package config

import (
	"time"
)

type SourceName string
type FilterName string
type CredentialName string
type Duration time.Duration

// Document is the mutable wire representation accepted at the parsing boundary.
type Document struct {
	Version int                       `json:"version" yaml:"version"`
	Server  Server                    `json:"server" yaml:"server"`
	Scan    Scan                      `json:"scan" yaml:"scan"`
	Sources map[SourceName]Source     `json:"sources" yaml:"sources"`
	Filters map[FilterName]FilterSpec `json:"filters" yaml:"filters"`
}

type Server struct {
	Listen string `json:"listen" yaml:"listen"`
}
type Scan struct {
	Interval Duration `json:"interval" yaml:"interval"`
	Timeout  Duration `json:"timeout" yaml:"timeout"`
}
type Source struct {
	Owners     []string       `json:"owners" yaml:"owners"`
	Credential CredentialName `json:"credential" yaml:"credential"`
}
type FilterSpec struct {
	Sources []SourceName `json:"sources" yaml:"sources"`
	Include []Rule       `json:"include" yaml:"include"`
	Exclude []Rule       `json:"exclude,omitempty" yaml:"exclude,omitempty"`
}
type Rule struct {
	Topics       *Topics  `json:"topics,omitempty" yaml:"topics,omitempty"`
	Repositories []string `json:"repositories,omitempty" yaml:"repositories,omitempty"`
	NameRegex    string   `json:"nameRegex,omitempty" yaml:"nameRegex,omitempty"`
}
type Topics struct {
	All []string `json:"all,omitempty" yaml:"all,omitempty"`
	Any []string `json:"any,omitempty" yaml:"any,omitempty"`
}

// Config is a parsed immutable configuration. Its zero value is uninitialized.
// Document and collection accessors return independent caller-owned snapshots.
type Config struct {
	state *configState
}

type configState struct {
	document Document
	filters  map[FilterName]Filter
}

// Filter contains predicates prepared once at the configuration boundary.
// It cannot be constructed with unchecked rules by downstream consumers.
type Filter struct {
	state *filterState
}

// Runtime binds a parsed configuration to frozen environment overrides and
// available credential names. It contains no secret values or providers.
type Runtime struct {
	state *runtimeState
}

type runtimeState struct {
	declared  Config
	effective Config
	bindings  *runtimeBindings
}

type runtimeBindings struct {
	overrides   map[string]string
	credentials map[CredentialName]struct{}
}
