package github

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
)

const maxPages = 1000

type repositoryDTO struct {
	ID       *int64  `json:"id"`
	Name     string  `json:"name"`
	FullName *string `json:"full_name"`
	Owner    *struct {
		Login string `json:"login"`
	} `json:"owner"`
	Archived *bool     `json:"archived"`
	Fork     *bool     `json:"fork"`
	Topics   *[]string `json:"topics"`
}

func (s *scan) pages(ctx context.Context, name config.CredentialName, path string, installation bool) ([]repositoryDTO, error) {
	current := apiOrigin + path
	initial, err := trustedURL(current)
	if err != nil {
		return nil, err
	}
	initialQuery := initial.Query()
	initialQuery.Del("page")
	visited := make(map[string]bool)
	repositories := make([]repositoryDTO, 0)
	total := -1
	for page := 0; page < maxPages; page++ {
		parsed, err := trustedURL(current)
		if err != nil || parsed.Path != initial.Path || parsed.RawPath != initial.RawPath {
			return nil, apiError("invalid_pagination_url", 0, "")
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			return nil, apiError("invalid_pagination_url", 0, "")
		}
		pageValues := query["page"]
		if len(pageValues) != 1 || pageValues[0] != strconv.Itoa(page+1) {
			return nil, apiError("invalid_pagination_sequence", 0, "")
		}
		scope := parsed.Query()
		scope.Del("page")
		if scope.Encode() != initialQuery.Encode() {
			return nil, apiError("pagination_scope_changed", 0, "")
		}
		parsed.RawQuery = query.Encode()
		canonical := parsed.String()
		if visited[canonical] {
			return nil, apiError("pagination_cycle", 0, "")
		}
		visited[canonical] = true
		res, err := s.get(ctx, name, current, false)
		if err != nil {
			return nil, err
		}
		var batch []repositoryDTO
		if installation {
			var envelope struct {
				Total        *int            `json:"total_count"`
				Repositories []repositoryDTO `json:"repositories"`
			}
			if err := decodeJSON(res.body, &envelope); err != nil || envelope.Total == nil || *envelope.Total < 0 || envelope.Repositories == nil {
				return nil, apiError("invalid_repository_page", 0, res.requestID)
			}
			if total >= 0 && total != *envelope.Total {
				return nil, apiError("catalog_changed_during_pagination", 0, res.requestID)
			}
			total = *envelope.Total
			batch = envelope.Repositories
		} else if err := decodeJSON(res.body, &batch); err != nil || batch == nil {
			return nil, apiError("invalid_repository_page", 0, res.requestID)
		}
		repositories = append(repositories, batch...)
		next, err := nextLink(res.link)
		if err != nil {
			return nil, err
		}
		if next == "" {
			if installation && total != len(repositories) {
				return nil, apiError("incomplete_installation_catalog", 0, res.requestID)
			}
			return repositories, nil
		}
		current = next
	}
	return nil, apiError("pagination_limit_exceeded", 0, "")
}

func nextLink(header string) (string, error) {
	var next string
	for strings.TrimSpace(header) != "" {
		header = strings.TrimSpace(header)
		if !strings.HasPrefix(header, "<") {
			return "", apiError("invalid_pagination_link", 0, "")
		}
		end := strings.IndexByte(header, '>')
		if end < 0 {
			return "", apiError("invalid_pagination_link", 0, "")
		}
		target := header[1:end]
		params := header[end+1:]
		quoted := false
		cut := len(params)
		for i, r := range params {
			if r == '"' {
				quoted = !quoted
			}
			if r == ',' && !quoted {
				cut = i
				break
			}
		}
		if quoted {
			return "", apiError("invalid_pagination_link", 0, "")
		}
		relations := ""
		for _, field := range strings.Split(params[:cut], ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
			if ok && strings.EqualFold(strings.TrimSpace(key), "rel") {
				relations += " " + strings.Trim(strings.TrimSpace(value), `"`)
			}
		}
		if strings.TrimSpace(relations) == "" {
			return "", apiError("invalid_pagination_link", 0, "")
		}
		if slices.Contains(strings.Fields(relations), "next") {
			if next != "" {
				return "", apiError("ambiguous_pagination_link", 0, "")
			}
			if _, err := trustedURL(target); err != nil {
				return "", err
			}
			next = target
		}
		if cut == len(params) {
			header = ""
		} else {
			header = params[cut+1:]
		}
	}
	return next, nil
}

func (s *scan) complete(ctx context.Context, name config.CredentialName, owner string, records []repositoryDTO) ([]discovery.Repository, error) {
	if err := validateRecords(records); err != nil {
		return nil, err
	}
	repositories := make([]discovery.Repository, 0, len(records))
	observations := make([]repositoryDTO, 0, len(records))
	for _, record := range records {
		if !strings.EqualFold(record.Owner.Login, owner) {
			continue
		}
		if *record.Archived || *record.Fork {
			observations = append(observations, record)
			continue
		}
		if record.Topics == nil {
			res, err := s.get(ctx, name, "/repos/"+url.PathEscape(record.Owner.Login)+"/"+url.PathEscape(record.Name), false)
			if err != nil {
				return nil, err
			}
			var complete repositoryDTO
			if err := decodeJSON(res.body, &complete); err != nil || !validRepository(complete) ||
				*complete.ID != *record.ID || complete.Owner.Login != record.Owner.Login || complete.Name != record.Name ||
				*complete.Fork != *record.Fork || *complete.Archived != *record.Archived || complete.Topics == nil {
				return nil, apiError("incomplete_repository_metadata", 0, res.requestID)
			}
			record = complete
		}
		topics := make([]string, 0, len(*record.Topics))
		for _, topic := range *record.Topics {
			if topic == "" || strings.TrimSpace(topic) != topic || strings.ContainsAny(topic, "\r\n\t ") {
				return nil, apiError("invalid_repository_topics", 0, "")
			}
			topics = append(topics, strings.ToLower(topic))
		}
		slices.Sort(topics)
		topics = slices.Compact(topics)
		observations = append(observations, record)
		repositories = append(repositories, discovery.Repository{ID: discovery.RepositoryID(*record.ID), Owner: record.Owner.Login, Name: record.Name, Topics: topics})
	}
	s.observations[catalogKey{credential: name, owner: strings.ToLower(owner)}] = observations
	return mergeRepositories(repositories)
}

func validateCatalogOwner(records []repositoryDTO, owner string) error {
	for _, record := range records {
		if record.Owner == nil || !strings.EqualFold(record.Owner.Login, owner) {
			return apiError("unexpected_repository_owner", 0, "")
		}
	}
	return nil
}

// Check identity and contradictory flags before archived/fork exclusion. A
// skipped second observation must not make a conflicting first observation
// look authoritative. Missing topics are completed later for active records.
func validateRecords(records []repositoryDTO) error {
	seen := make(map[int64]repositoryDTO, len(records))
	for _, record := range records {
		if !validRepository(record) {
			return apiError("invalid_repository_metadata", 0, "")
		}
		if previous, ok := seen[*record.ID]; ok {
			if conflictingRecords(previous, record) {
				return apiError("conflicting_repository_metadata", 0, "")
			}
			if record.Topics == nil {
				continue
			}
		}
		seen[*record.ID] = record
	}
	return nil
}

func conflictingRecords(a, b repositoryDTO) bool {
	return a.Owner.Login != b.Owner.Login || a.Name != b.Name || *a.Archived != *b.Archived || *a.Fork != *b.Fork ||
		(a.Topics != nil && b.Topics != nil && !slices.Equal(normalizeTopics(*a.Topics), normalizeTopics(*b.Topics)))
}

// Reconcile even the archived/fork observations that never become returned
// repositories. Both conflicting sources are untrustworthy; unrelated sources
// continue working. These observations live only inside this Scan.
func reconcileObservations(results map[config.SourceName]Result, observations map[config.SourceName][]repositoryDTO) {
	type observed struct {
		record   repositoryDTO
		sources  map[config.SourceName]bool
		conflict bool
	}
	seen := make(map[int64]*observed)
	for name, records := range observations {
		for _, record := range records {
			previous, ok := seen[*record.ID]
			if !ok {
				seen[*record.ID] = &observed{record: record, sources: map[config.SourceName]bool{name: true}}
				continue
			}
			previous.sources[name] = true
			if conflictingRecords(previous.record, record) {
				previous.conflict = true
			}
			if previous.record.Topics == nil && record.Topics != nil {
				previous.record = record
			}
		}
	}
	for _, observation := range seen {
		if observation.conflict {
			for name := range observation.sources {
				results[name] = Result{Err: apiError("conflicting_repository_metadata", 0, "")}
			}
		}
	}
}

func normalizeTopics(topics []string) []string {
	result := make([]string, len(topics))
	for i, topic := range topics {
		result[i] = strings.ToLower(topic)
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func validRepository(record repositoryDTO) bool {
	if record.ID == nil || *record.ID <= 0 || record.Owner == nil || !ownerPattern.MatchString(record.Owner.Login) ||
		!repositoryPattern.MatchString(record.Name) || record.Name == "." || record.Name == ".." || record.Archived == nil || record.Fork == nil {
		return false
	}
	return record.FullName == nil || *record.FullName == record.Owner.Login+"/"+record.Name
}

func mergeRepositories(input []discovery.Repository) ([]discovery.Repository, error) {
	unique := make(map[discovery.RepositoryID]discovery.Repository, len(input))
	for _, repository := range input {
		if previous, ok := unique[repository.ID]; ok && (previous.Owner != repository.Owner || previous.Name != repository.Name ||
			previous.Archived != repository.Archived || previous.Fork != repository.Fork || !slices.Equal(previous.Topics, repository.Topics)) {
			return nil, apiError("conflicting_repository_metadata", 0, "")
		}
		repository.Topics = slices.Clone(repository.Topics)
		unique[repository.ID] = repository
	}
	result := make([]discovery.Repository, 0, len(unique))
	for _, repository := range unique {
		result = append(result, repository)
	}
	slices.SortFunc(result, func(a, b discovery.Repository) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return result, nil
}

// jsontext enforces JSON syntax, UTF-8 and exact duplicate-name rejection.
// The additional pass preserves our depth bound and case-alias rejection,
// including aliases among unknown GitHub fields, before typed v2 decoding.
func decodeJSON(data []byte, target any) error {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	if err := validateJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.ReadToken(); err != io.EOF {
		return apiError("invalid_json", 0, "")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return apiError("invalid_json", 0, "")
	}
	return nil
}

func validateJSONValue(decoder *jsontext.Decoder, depth int) error {
	if depth > 100 {
		return apiError("invalid_json", 0, "")
	}
	token, err := decoder.ReadToken()
	if err != nil {
		return apiError("invalid_json", 0, "")
	}
	switch token.Kind() {
	case '{':
		seen := make(map[string]string)
		for decoder.PeekKind() != '}' {
			key, err := decoder.ReadToken()
			if err != nil || key.Kind() != '"' {
				return apiError("invalid_json", 0, "")
			}
			name := key.String()
			folded := strings.ToLower(name)
			// Strict v2 matching would ignore ID alongside id as an unknown
			// field. Keep rejecting that ambiguity under the catalog contract.
			if previous, exists := seen[folded]; exists && previous != name {
				return apiError("invalid_json", 0, "")
			}
			seen[folded] = name
			if err := validateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.PeekKind() != ']' {
			if err := validateJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	if _, err := decoder.ReadToken(); err != nil {
		return apiError("invalid_json", 0, "")
	}
	return nil
}
