package discovery_test

import (
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
)

// A complete upstream catalog still must have valid required GitHub identity.
// The same owner/name syntax applies to explicit config names and API records.
func TestMalformedRequiredIdentityFailsWholeFilter(t *testing.T) {
	for _, bad := range []discovery.Repository{
		{ID: 1, Owner: " ", Name: "repo"},
		{ID: 1, Owner: "acme\n", Name: "repo"},
		{ID: 1, Owner: "-", Name: "repo"},
		{ID: 1, Owner: "acme", Name: " "},
		{ID: 1, Owner: "acme", Name: ".."},
		{ID: 1, Owner: "acme", Name: "bad\nname"},
	} {
		result, err := discovery.Evaluate(mustFilter(t, config.FilterSpec{Sources: []config.SourceName{"s"}, Include: []config.Rule{{NameRegex: ".*"}}}), map[config.SourceName][]discovery.Repository{"s": {repo(2, "valid"), bad}})
		if err == nil || len(result.Inputs) != 0 {
			t.Errorf("malformed required repository identity published: %+v result=%+v error=%v", bad, result, err)
		}
	}
}
