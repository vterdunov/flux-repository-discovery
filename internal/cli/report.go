package cli

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"slices"

	"github.com/vterdunov/flux-repository-discovery/internal/jsonwire"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

// Rendering buffers the whole report before stdout so failed requests cannot
// produce something that looks like a successful partial preview.
func renderReport(report service.Report) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "Base: %s (generation %d)\nCandidate: %s\nObserved: %s\nChanges: %t\n",
		report.BaseRevision, report.BaseGeneration, report.CandidateRevision, report.ObservedAt.Format("2006-01-02T15:04:05Z07:00"), report.HasChanges)
	for _, group := range []struct {
		name    string
		changes []service.Change
	}{{"Declared configuration", report.ConfigChanges}, {"Effective configuration", report.EffectiveChanges}} {
		fmt.Fprintf(&out, "\n%s:\n", group.name)
		if len(group.changes) == 0 {
			out.WriteString("  no changes\n")
		}
		for _, change := range group.changes {
			fmt.Fprintf(&out, "  %s %s: %s -> %s\n", change.Operation, change.Path, jsonValue(change.Before), jsonValue(change.After))
		}
	}
	out.WriteString("\nServer environment overrides:\n")
	for _, name := range sortedKeys(report.Overrides) {
		fmt.Fprintf(&out, "  %s=%s\n", name, report.Overrides[name])
	}
	out.WriteString("\nFilter changes on the same fresh catalog:\n")
	for _, name := range sortedKeys(report.Filters) {
		diff := report.Filters[name]
		fmt.Fprintf(&out, "\n/inputs/%s: %s\n", name, diff.Kind)
		if diff.Kind == "removed" {
			out.WriteString("  endpoint will return HTTP 404 after restart\n")
		}
		renderDiff(&out, diff)
	}
	out.WriteString("\nGitHub drift since published generation:\n")
	for _, name := range sortedKeys(report.Drift) {
		drift := report.Drift[name]
		fmt.Fprintf(&out, "\n/inputs/%s:\n", name)
		if !drift.Available {
			out.WriteString("  comparison unavailable (no successful published result)\n")
			continue
		}
		renderDiff(&out, drift.Diff)
	}
	return out.Bytes()
}

func renderDiff(out *bytes.Buffer, diff service.FilterDiff) {
	fmt.Fprintf(out, "  %d -> %d repositories; %d unchanged\n", diff.BeforeCount, diff.AfterCount, diff.Unchanged)
	for _, input := range diff.Added {
		fmt.Fprintf(out, "  + %s\n", jsonValue(marshal(input)))
	}
	for _, input := range diff.Removed {
		fmt.Fprintf(out, "  - %s\n", jsonValue(marshal(input)))
	}
	for _, input := range diff.Changed {
		fmt.Fprintf(out, "  ~ %s -> %s\n", jsonValue(marshal(input.Before)), jsonValue(marshal(input.After)))
	}
	for _, name := range diff.UnmatchedRepositories {
		fmt.Fprintf(out, "  unmatched repository rule: %s\n", name)
	}
	out.WriteString("  complete resulting inputs:\n")
	for _, input := range diff.Inputs {
		fmt.Fprintf(out, "    %s\n", jsonValue(marshal(input)))
	}
	if len(diff.Inputs) == 0 {
		out.WriteString("    []\n")
	}
}

func marshal(value any) jsontext.Value {
	data, _ := jsonwire.Marshal(value) // Only concrete report structs reach this helper.
	return data
}

func jsonValue(value jsontext.Value) string {
	if len(value) == 0 {
		return "(absent)"
	}
	return string(value)
}

func sortedKeys[K ~string, V any](values map[K]V) []K {
	keys := make([]K, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
