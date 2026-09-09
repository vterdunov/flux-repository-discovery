package config

import (
	"regexp"
	"slices"
	"strings"
)

type filterState struct {
	sources      []SourceName
	include      []preparedRule
	exclude      []preparedRule
	repositories []string
}

type preparedRule struct {
	rule    Rule
	pattern *regexp.Regexp
}

func (f Filter) IsZero() bool { return f.state == nil }

func (f Filter) Sources() []SourceName {
	if f.IsZero() {
		return nil
	}
	return slices.Clone(f.state.sources)
}

// Repositories returns include references followed by exclude references in
// declaration order. It preserves spelling and duplicates for diagnostics.
func (f Filter) Repositories() []string {
	if f.IsZero() {
		return nil
	}
	return slices.Clone(f.state.repositories)
}

// Matches evaluates only the prepared predicates. Catalog validity and the
// unconditional archived/fork exclusions remain the discovery boundary's job.
func (f Filter) Matches(owner, name string, topics []string) bool {
	return !f.IsZero() && matchesAny(f.state.include, owner, name, topics) && !matchesAny(f.state.exclude, owner, name, topics)
}

func matchesAny(rules []preparedRule, owner, name string, topics []string) bool {
	for _, rule := range rules {
		if rule.matches(owner, name, topics) {
			return true
		}
	}
	return false
}

func (prepared preparedRule) matches(owner, name string, topics []string) bool {
	rule := prepared.rule
	if prepared.pattern != nil && !prepared.pattern.MatchString(name) {
		return false
	}
	if rule.Repositories != nil && !containsFold(rule.Repositories, owner+"/"+name) {
		return false
	}
	if rule.Topics != nil {
		for _, topic := range rule.Topics.All {
			if !containsFold(topics, topic) {
				return false
			}
		}
		if rule.Topics.Any != nil {
			matched := false
			for _, topic := range rule.Topics.Any {
				if containsFold(topics, topic) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
	}
	return true
}

func containsFold(values []string, wanted string) bool {
	return slices.ContainsFunc(values, func(value string) bool { return strings.EqualFold(value, wanted) })
}
