package config

import (
	"errors"
	"fmt"
	"math"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vterdunov/flux-repository-discovery/internal/jsonwire"
)

var (
	namePattern       = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	ownerPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	topicPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,49}$`)
)

// Parse validates and snapshots a complete declaration, preparing each regexp
// once. Defaults belong to Decode; explicit zero scalar settings are invalid.
func Parse(document Document) (Config, error) {
	cfg := cloneDocument(document)
	filters, err := prepareDocument(cfg)
	if err != nil {
		return Config{}, err
	}
	// The normalized document crosses the same JSON boundary at startup and
	// dry-run. Check that representation once here without decoding it again
	// or rebuilding the predicates already prepared above.
	wire, err := jsonwire.Marshal(cfg)
	if err != nil {
		return Config{}, errors.New("configuration: cannot encode canonical document")
	}
	if len(wire) > MaxBytes {
		return Config{}, errors.New("configuration canonical document exceeds size limit")
	}
	return Config{state: &configState{document: cfg, filters: filters}}, nil
}

func prepareDocument(cfg Document) (map[FilterName]Filter, error) {
	if cfg.Version != 1 {
		return nil, errors.New("version: must be 1")
	}
	if err := validateListen(cfg.Server.Listen); err != nil {
		return nil, fmt.Errorf("server.listen: %w", err)
	}
	if cfg.Scan.Interval <= 0 {
		return nil, errors.New("scan.interval: must be positive")
	}
	if cfg.Scan.Timeout <= 0 {
		return nil, errors.New("scan.timeout: must be positive")
	}
	if int64(cfg.Scan.Interval) > math.MaxInt64-int64(cfg.Scan.Timeout) {
		return nil, errors.New("scan: interval plus timeout overflows duration")
	}
	for _, name := range sortedKeys(cfg.Sources) {
		source := cfg.Sources[name]
		path := "sources." + string(name)
		if !namePattern.MatchString(string(name)) {
			return nil, fmt.Errorf("%s: invalid name", path)
		}
		if !namePattern.MatchString(string(source.Credential)) {
			return nil, fmt.Errorf("%s.credential: invalid credential name", path)
		}
		if len(source.Owners) == 0 {
			return nil, fmt.Errorf("%s.owners: must not be empty", path)
		}
		for i, owner := range source.Owners {
			if !ownerPattern.MatchString(owner) {
				return nil, fmt.Errorf("%s.owners[%d]: invalid GitHub owner", path, i)
			}
		}
	}
	filters := make(map[FilterName]Filter, len(cfg.Filters))
	for _, name := range sortedKeys(cfg.Filters) {
		filter := cfg.Filters[name]
		path := "filters." + string(name)
		if !namePattern.MatchString(string(name)) {
			return nil, fmt.Errorf("%s: invalid name", path)
		}
		prepared, err := prepareFilter(filter)
		if err != nil {
			return nil, fmt.Errorf("%s.%w", path, err)
		}
		filters[name] = prepared
		for i, source := range filter.Sources {
			if _, ok := cfg.Sources[source]; !ok {
				return nil, fmt.Errorf("%s.sources[%d]: unknown source %q", path, i, source)
			}
		}
	}
	return filters, nil
}

func validateListen(value string) error {
	if !utf8.ValidString(value) {
		return errors.New("must be valid UTF-8")
	}
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return errors.New("must be a host:port address")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return errors.New("port must be between 0 and 65535")
	}
	return nil
}

// prepareFilter validates rules and retains their compiled predicates. Its
// input belongs to the private document snapshot, so the prepared state may
// safely share that document's immutable rule slices.
func prepareFilter(filter FilterSpec) (Filter, error) {
	if len(filter.Sources) == 0 {
		return Filter{}, errors.New("sources: must not be empty")
	}
	if len(filter.Include) == 0 {
		return Filter{}, errors.New("include: must contain at least one rule")
	}
	for i, name := range filter.Sources {
		if !namePattern.MatchString(string(name)) {
			return Filter{}, fmt.Errorf("sources[%d]: invalid source name", i)
		}
	}
	state := &filterState{sources: filter.Sources}
	for _, group := range []struct {
		name   string
		rules  []Rule
		target *[]preparedRule
	}{{"include", filter.Include, &state.include}, {"exclude", filter.Exclude, &state.exclude}} {
		for i, rule := range group.rules {
			prepared, err := prepareRule(rule)
			if err != nil {
				return Filter{}, fmt.Errorf("%s[%d]%w", group.name, i, err)
			}
			*group.target = append(*group.target, prepared)
			state.repositories = append(state.repositories, rule.Repositories...)
		}
	}
	return Filter{state: state}, nil
}

func prepareRule(rule Rule) (preparedRule, error) {
	if rule.Topics == nil && rule.Repositories == nil && rule.NameRegex == "" {
		return preparedRule{}, errors.New(": empty rule")
	}
	prepared := preparedRule{rule: rule}
	if rule.NameRegex != "" {
		pattern, err := regexp.Compile(rule.NameRegex)
		if err != nil {
			return preparedRule{}, errors.New(".nameRegex: invalid regular expression")
		}
		prepared.pattern = pattern
	}
	if rule.Repositories != nil {
		if len(rule.Repositories) == 0 {
			return preparedRule{}, errors.New(".repositories: must not be empty")
		}
		for i, name := range rule.Repositories {
			if !ValidRepositoryFullName(name) {
				return preparedRule{}, fmt.Errorf(".repositories[%d]: expected owner/name", i)
			}
		}
	}
	if rule.Topics != nil {
		if rule.Topics.All == nil && rule.Topics.Any == nil {
			return preparedRule{}, errors.New(".topics: all or any is required")
		}
		for _, group := range []struct {
			name   string
			values []string
		}{{"all", rule.Topics.All}, {"any", rule.Topics.Any}} {
			if group.values == nil {
				continue
			}
			if len(group.values) == 0 {
				return preparedRule{}, fmt.Errorf(".topics.%s: must not be empty", group.name)
			}
			for i, topic := range group.values {
				if !topicPattern.MatchString(topic) {
					return preparedRule{}, fmt.Errorf(".topics.%s[%d]: invalid topic", group.name, i)
				}
			}
		}
	}
	return prepared, nil
}

// ValidRepositoryFullName is shared by configuration and catalog validation.
func ValidRepositoryFullName(name string) bool {
	parts := strings.Split(name, "/")
	return len(parts) == 2 && ownerPattern.MatchString(parts[0]) && repositoryPattern.MatchString(parts[1]) && parts[1] != "." && parts[1] != ".."
}

func sortedKeys[K ~string, V any](values map[K]V) []K {
	keys := make([]K, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
