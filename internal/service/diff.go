package service

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"slices"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/jsonwire"
)

func diffConfig(before, after config.Config) []Change {
	old, _ := jsonwire.Marshal(before.Document())
	proposed, _ := jsonwire.Marshal(after.Document())
	changes := []Change{}
	compareJSON(old, proposed, "", &changes)
	return changes
}

// Arrays are replaced as complete values: order is visible and no condition is
// omitted from the report. Dynamic JSON is confined to this serialization edge.
func compareJSON(before, after jsontext.Value, path string, changes *[]Change) {
	if bytes.Equal(before, after) {
		return
	}
	if len(before) != 0 && len(after) != 0 && before[0] == '{' && after[0] == '{' {
		var old, proposed map[string]jsontext.Value
		if json.Unmarshal(before, &old) == nil && json.Unmarshal(after, &proposed) == nil {
			all := make(map[string]bool)
			for key := range old {
				all[key] = true
			}
			for key := range proposed {
				all[key] = true
			}
			for _, key := range keys(all) {
				child := key
				if path != "" {
					child = path + "." + key
				}
				compareJSON(old[key], proposed[key], child, changes)
			}
			return
		}
	}
	operation := Operation("replace")
	if before == nil {
		operation = "add"
	} else if after == nil {
		operation = "remove"
	}
	*changes = append(*changes, Change{Operation: operation, Path: path, Before: before, After: after})
}

func diffInputs(before, after []discovery.Input) FilterDiff {
	diff := FilterDiff{Kind: "unchanged", BeforeCount: len(before), AfterCount: len(after), Added: []discovery.Input{}, Removed: []discovery.Input{}, Changed: []InputChange{}, Inputs: append([]discovery.Input{}, after...), UnmatchedRepositories: []string{}}
	old := make(map[string]discovery.Input)
	proposed := make(map[string]discovery.Input)
	all := make(map[string]bool)
	for _, input := range before {
		old[input.ID] = input
		all[input.ID] = true
	}
	for _, input := range after {
		proposed[input.ID] = input
		all[input.ID] = true
	}
	ids := keys(all)
	slices.SortFunc(ids, func(a, b string) int {
		if order := cmp.Compare(len(a), len(b)); order != 0 {
			return order
		}
		return cmp.Compare(a, b)
	})
	for _, id := range ids {
		a, oldExists := old[id]
		b, newExists := proposed[id]
		switch {
		case !oldExists:
			diff.Added = append(diff.Added, b)
		case !newExists:
			diff.Removed = append(diff.Removed, a)
		case a != b:
			diff.Changed = append(diff.Changed, InputChange{Before: a, After: b})
		default:
			diff.Unchanged++
		}
	}
	if len(diff.Added)+len(diff.Removed)+len(diff.Changed) > 0 {
		diff.Kind = "modified"
	}
	return diff
}
