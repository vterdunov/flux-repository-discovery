package config_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
)

func sizedPreparedDocument(ownerCount int) config.Document {
	owners := make([]string, ownerCount)
	for i := range owners {
		owners[i] = strings.Repeat("a", 39)
	}
	return config.Document{
		Version: 1,
		Server:  config.Server{Listen: ":8080"},
		Scan:    config.Scan{Interval: config.Duration(time.Minute), Timeout: config.Duration(time.Second)},
		Sources: map[config.SourceName]config.Source{"large": {Owners: owners, Credential: "token"}},
		Filters: map[config.FilterName]config.FilterSpec{},
	}
}

// The previous startup/dry-run boundary rejected normalized JSON over MaxBytes.
// That invariant now belongs to config, before a usable Config can escape.
func TestParsePreservesCanonicalDocumentSizeLimit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ownerCount int
		oversize   bool
	}{
		{"under limit", 20000, false},
		{"over limit", 26000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document := sizedPreparedDocument(tc.ownerCount)
			// An independent JSON consumer determines fixture size, avoiding
			// dependence on the production serializer's implementation.
			wire, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if (len(wire) > config.MaxBytes) != tc.oversize {
				t.Fatalf("fixture has unexpected size %d (limit %d)", len(wire), config.MaxBytes)
			}
			cfg, err := config.Parse(document)
			if tc.oversize {
				if err == nil || !cfg.IsZero() || !strings.Contains(err.Error(), "size") {
					t.Fatalf("oversize declaration escaped config boundary: zero=%t error=%v", cfg.IsZero(), err)
				}
				return
			}
			if err != nil || cfg.IsZero() {
				t.Fatalf("bounded declaration rejected: zero=%t error=%v", cfg.IsZero(), err)
			}
			if runtime, err := config.Prepare(cfg, nil, []config.CredentialName{"token"}); err != nil || runtime.IsZero() {
				t.Fatalf("bounded declaration cannot be prepared: zero=%t error=%v", runtime.IsZero(), err)
			}
		})
	}
}

func TestDecodeRejectsCanonicalExpansionBeforeReturningPreparedConfig(t *testing.T) {
	owner := strings.Repeat("a", 39)
	input := "version: 1\nsources:\n  large:\n    credential: token\n    owners: [" + strings.Repeat(owner+",", 25999) + owner + "]\nfilters: {}\n"
	if len(input) > config.MaxBytes {
		t.Fatalf("compact input fixture must fit the byte limit: %d", len(input))
	}
	cfg, err := config.Decode([]byte(input))
	if err == nil || !cfg.IsZero() || !strings.Contains(err.Error(), "size") {
		t.Fatalf("canonical expansion escaped config boundary: inputBytes=%d zero=%t error=%v", len(input), cfg.IsZero(), err)
	}
}
