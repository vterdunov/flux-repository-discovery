package config

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"time"
)

// Prepare binds only documented scalar overrides and available credential
// names. It neither reads secrets nor recompiles already prepared predicates.
func Prepare(cfg Config, env map[string]string, names []CredentialName) (Runtime, error) {
	bindings := &runtimeBindings{overrides: make(map[string]string), credentials: make(map[CredentialName]struct{}, len(names))}
	for _, key := range []string{"FRD_LISTEN", "FRD_SCAN_INTERVAL", "FRD_SCAN_TIMEOUT"} {
		if value, ok := env[key]; ok {
			bindings.overrides[key] = value
		}
	}
	for _, name := range names {
		bindings.credentials[name] = struct{}{}
	}
	return prepareRuntime(cfg, bindings)
}

func prepareRuntime(cfg Config, bindings *runtimeBindings) (Runtime, error) {
	if cfg.IsZero() {
		return Runtime{}, errors.New("configuration: parsed configuration is required")
	}
	for _, name := range sortedKeys(cfg.state.document.Sources) {
		source := cfg.state.document.Sources[name]
		if _, ok := bindings.credentials[source.Credential]; !ok {
			return Runtime{}, fmt.Errorf("sources.%s.credential: unknown credential %q", name, source.Credential)
		}
	}
	// Copy only the immutable state's scalar container. Document collections
	// and prepared filter states remain safely shared with the declaration.
	effective := *cfg.state
	if value, ok := bindings.overrides["FRD_LISTEN"]; ok {
		if err := validateListen(value); err != nil {
			return Runtime{}, fmt.Errorf("FRD_LISTEN: %w", err)
		}
		effective.document.Server.Listen = value
	}
	for _, setting := range []struct {
		name   string
		target *Duration
	}{{"FRD_SCAN_INTERVAL", &effective.document.Scan.Interval}, {"FRD_SCAN_TIMEOUT", &effective.document.Scan.Timeout}} {
		if value, ok := bindings.overrides[setting.name]; ok {
			parsed, err := time.ParseDuration(value)
			if err != nil || parsed <= 0 {
				return Runtime{}, fmt.Errorf("%s: must be a positive duration", setting.name)
			}
			*setting.target = Duration(parsed)
		}
	}
	if int64(effective.document.Scan.Interval) > math.MaxInt64-int64(effective.document.Scan.Timeout) {
		return Runtime{}, errors.New("scan: interval plus timeout overflows duration")
	}
	return Runtime{state: &runtimeState{declared: cfg, effective: Config{state: &effective}, bindings: bindings}}, nil
}

func (r Runtime) IsZero() bool { return r.state == nil }

func (r Runtime) Declared() Config {
	if r.IsZero() {
		return Config{}
	}
	return r.state.declared
}

func (r Runtime) Effective() Config {
	if r.IsZero() {
		return Config{}
	}
	return r.state.effective
}

func (r Runtime) Overrides() map[string]string {
	if r.IsZero() {
		return nil
	}
	return maps.Clone(r.state.bindings.overrides)
}

func (r Runtime) PrepareCandidate(candidate Config) (Runtime, error) {
	if r.IsZero() {
		return Runtime{}, errors.New("runtime: prepared runtime is required")
	}
	return prepareRuntime(candidate, r.state.bindings)
}
