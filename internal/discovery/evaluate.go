package discovery

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
)

// Evaluate computes an entire result or returns an error with no partial inputs.
func Evaluate(filter config.Filter, catalogs map[config.SourceName][]Repository) (Result, error) {
	if filter.IsZero() {
		return Result{}, fmt.Errorf("filter is not initialized")
	}
	repositories := make(map[RepositoryID]Repository)
	availableNames := make(map[string]bool)
	for _, source := range filter.Sources() {
		catalog, ok := catalogs[source]
		if !ok {
			return Result{}, fmt.Errorf("source %q: catalog unavailable", source)
		}
		for _, repository := range catalog {
			if repository.ID <= 0 || !config.ValidRepositoryFullName(repository.Owner+"/"+repository.Name) {
				return Result{}, fmt.Errorf("source %q: invalid required repository metadata", source)
			}
			if previous, exists := repositories[repository.ID]; exists && !sameRepository(previous, repository) {
				return Result{}, fmt.Errorf("repository %d: conflicting source metadata", repository.ID)
			}
			repositories[repository.ID] = repository
			availableNames[strings.ToLower(repository.Owner+"/"+repository.Name)] = true
		}
	}
	ids := make([]RepositoryID, 0, len(repositories))
	for id := range repositories {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	result := Result{Inputs: []Input{}, UnmatchedRepositories: []string{}}
	for _, id := range ids {
		repository := repositories[id]
		if repository.Archived || repository.Fork || !filter.Matches(repository.Owner, repository.Name, repository.Topics) {
			continue
		}
		result.Inputs = append(result.Inputs, Input{ID: strconv.FormatInt(int64(id), 10), FullName: repository.Owner + "/" + repository.Name, Owner: repository.Owner, Name: repository.Name})
	}
	unmatched := make(map[string]bool)
	for _, name := range filter.Repositories() {
		name = strings.ToLower(name)
		if !availableNames[name] {
			unmatched[name] = true
		}
	}
	for name := range unmatched {
		result.UnmatchedRepositories = append(result.UnmatchedRepositories, name)
	}
	slices.Sort(result.UnmatchedRepositories)
	return result, nil
}

func sameRepository(a, b Repository) bool {
	return a.ID == b.ID && a.Owner == b.Owner && a.Name == b.Name && a.Archived == b.Archived && a.Fork == b.Fork && slices.Equal(normalizedTopics(a.Topics), normalizedTopics(b.Topics))
}

func normalizedTopics(topics []string) []string {
	result := make([]string, len(topics))
	for i, topic := range topics {
		result[i] = strings.ToLower(topic)
	}
	slices.Sort(result)
	return slices.Compact(result)
}
