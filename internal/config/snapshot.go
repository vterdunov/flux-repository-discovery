package config

import (
	"maps"
	"slices"
)

func (c Config) IsZero() bool { return c.state == nil }

func (c Config) Document() Document {
	if c.IsZero() {
		return Document{}
	}
	return cloneDocument(c.state.document)
}

func (c Config) Server() Server {
	if c.IsZero() {
		return Server{}
	}
	return c.state.document.Server
}

func (c Config) Scan() Scan {
	if c.IsZero() {
		return Scan{}
	}
	return c.state.document.Scan
}

func (c Config) Sources() map[SourceName]Source {
	if c.IsZero() {
		return nil
	}
	return cloneSources(c.state.document.Sources)
}

func (c Config) Filters() map[FilterName]Filter {
	if c.IsZero() {
		return nil
	}
	// Prepared filters themselves are immutable; only the returned map needs
	// copying. No regexp pointers or mutable rule objects cross this boundary.
	return maps.Clone(c.state.filters)
}

func cloneDocument(document Document) Document {
	document.Sources = cloneSources(document.Sources)
	filters := make(map[FilterName]FilterSpec, len(document.Filters))
	for name, spec := range document.Filters {
		spec.Sources = slices.Clone(spec.Sources)
		spec.Include = cloneRules(spec.Include)
		spec.Exclude = cloneRules(spec.Exclude)
		if len(spec.Exclude) == 0 {
			spec.Exclude = nil
		}
		filters[name] = spec
	}
	document.Filters = filters
	return document
}

func cloneSources(sources map[SourceName]Source) map[SourceName]Source {
	result := make(map[SourceName]Source, len(sources))
	for name, source := range sources {
		source.Owners = slices.Clone(source.Owners)
		result[name] = source
	}
	return result
}

func cloneRules(rules []Rule) []Rule {
	result := slices.Clone(rules)
	for i, rule := range result {
		rule.Repositories = slices.Clone(rule.Repositories)
		if rule.Topics != nil {
			rule.Topics = &Topics{All: slices.Clone(rule.Topics.All), Any: slices.Clone(rule.Topics.Any)}
		}
		result[i] = rule
	}
	return result
}
