package config_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
)

// An accepted normalized configuration must survive the CLI JSON boundary.
// Rejecting explicitly empty optional lists is also permitted by PLAN section 4.
func TestAcceptedEmptyOptionalListKeepsNormalizedJSONRoundTrip(t *testing.T) {
	input := `version: 1
sources: {company: {owners: [acme], credential: token}}
filters: {apps: {sources: [company], include: [{nameRegex: '.*'}], exclude: []}}
`
	cfg, err := config.Decode([]byte(input))
	if err != nil {
		return
	}
	wire, err := json.Marshal(cfg.Document())
	if err != nil {
		t.Fatal(err)
	}
	again, err := config.Decode(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Document(), again.Document()) {
		t.Fatalf("normalized config changes across dry-run JSON boundary: %#v -> %#v", cfg.Document().Filters["apps"], again.Document().Filters["apps"])
	}
}

func TestParserRejectsNullsAliasesAndOversizeBeforePublication(t *testing.T) {
	for _, tc := range []struct{ name, input, path string }{
		{"null sources", "version: 1\nsources: null\nfilters: {}", "sources"},
		{"null topic", "version: 1\nsources: {company: {owners: [acme], credential: token}}\nfilters: {apps: {sources: [company], include: [{topics: null}]}}", "filters.apps.include[0].topics"},
		{"alias", "version: 1\nsources: {company: &source {owners: [acme], credential: token}, copy: *source}\nfilters: {}", "sources.copy"},
		{"interval overflow", "version: 1\nscan: {interval: 2562047h, timeout: 2562047h}\nsources: {}\nfilters: {}", "scan"},
		{"empty listen", "version: 1\nserver: {listen: ''}\nsources: {}\nfilters: {}", "server.listen"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.Decode([]byte(tc.input))
			if err == nil || !strings.Contains(err.Error(), tc.path) {
				t.Fatalf("invalid input accepted or field path missing: %v", err)
			}
		})
	}
	if _, err := config.Decode([]byte(strings.Repeat(" ", config.MaxBytes+1))); err == nil {
		t.Fatal("oversize configuration accepted")
	}
}
