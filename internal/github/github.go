// Package github provides complete, authenticated GitHub repository catalogs.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
)

// Options allows deterministic transport and time at the external API boundary.
// Requests always target https://api.github.com; HTTPClient must not change that
// trust boundary. Wait must return promptly when ctx is canceled.
type Options struct {
	HTTPClient *http.Client
	Now        func() time.Time
	Wait       func(context.Context, time.Duration) error
}

// Result is a complete source catalog or an error, never a partial catalog.
type Result struct {
	Repositories []discovery.Repository
	Err          error
}

// Client shares credentials, installation tokens and rate-limit state between
// scans. A Scan reuses identical requests but never repositories from old scans.
type Client struct {
	credentials *Credentials
	http        *http.Client
	now         func() time.Time
	wait        func(context.Context, time.Duration) error
	states      map[config.CredentialName]*credentialState
}

func NewClient(credentials *Credentials, options Options) (*Client, error) {
	if credentials == nil || len(credentials.entries) == 0 {
		return nil, apiError("credentials_required", 0, "")
	}
	transport := defaultHTTPClient()
	if options.HTTPClient != nil {
		// Do not mutate the caller's client. Disable redirects and cookie jars:
		// only explicit credentials and GitHub endpoints are trusted.
		copyClient := *options.HTTPClient
		transport = &copyClient
		if transport.Timeout <= 0 || transport.Timeout > requestTimeout {
			transport.Timeout = requestTimeout
		}
	}
	transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport.Jar = nil
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Wait == nil {
		options.Wait = waitContext
	}
	client := &Client{credentials: credentials, http: transport, now: options.Now, wait: options.Wait,
		states: make(map[config.CredentialName]*credentialState, len(credentials.entries))}
	for name := range credentials.entries {
		client.states[name] = &credentialState{gate: make(chan struct{}, 1)}
	}
	return client, nil
}

type requestKey struct {
	credential config.CredentialName
	url        string
	app        bool
}

type cachedResponse struct {
	response response
	err      error
}

type catalogKey struct {
	credential config.CredentialName
	owner      string
}

type scan struct {
	client       *Client
	responses    map[requestKey]cachedResponse
	catalogs     map[catalogKey]Result
	observations map[catalogKey][]repositoryDTO
}

var ownerPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)
var repositoryPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

func (c *Client) Scan(ctx context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]Result {
	results := make(map[config.SourceName]Result, len(sources))
	groups := make(map[config.CredentialName]map[config.SourceName]config.Source)
	observations := make(map[config.SourceName][]repositoryDTO)
	// Validate every reference before any network request. Independent valid
	// sources still get a result even when another source is misconfigured.
	for name, source := range sources {
		if !c.credentials.Has(source.Credential) {
			results[name] = Result{Err: apiError("unknown_credential", 0, "")}
			continue
		}
		if !validOwners(source.Owners) {
			results[name] = Result{Err: apiError("invalid_owners", 0, "")}
			continue
		}
		if groups[source.Credential] == nil {
			groups[source.Credential] = make(map[config.SourceName]config.Source)
		}
		groups[source.Credential][name] = source
	}
	credentials := make([]config.CredentialName, 0, len(groups))
	for name := range groups {
		credentials = append(credentials, name)
	}
	slices.Sort(credentials)
	jobs := make(chan config.CredentialName, len(credentials))
	for _, name := range credentials {
		jobs <- name
	}
	close(jobs)
	var mu sync.Mutex
	var workers sync.WaitGroup
	// Separate credentials have independent budgets and can make progress
	// while another upstream blocks. Four workers bound goroutines and network
	// pressure; each group's private cache preserves same-scan request reuse.
	for range min(4, len(credentials)) {
		workers.Go(func() {
			for credential := range jobs {
				s := scan{client: c, responses: make(map[requestKey]cachedResponse), catalogs: make(map[catalogKey]Result), observations: make(map[catalogKey][]repositoryDTO)}
				group := groups[credential]
				names := make([]config.SourceName, 0, len(group))
				for name := range group {
					names = append(names, name)
				}
				slices.Sort(names)
				for _, name := range names {
					result := s.source(ctx, group[name])
					mu.Lock()
					results[name] = result
					if result.Err == nil {
						for _, owner := range group[name].Owners {
							key := catalogKey{credential: credential, owner: strings.ToLower(owner)}
							observations[name] = append(observations[name], s.observations[key]...)
						}
					}
					mu.Unlock()
				}
			}
		})
	}
	workers.Wait()
	reconcileObservations(results, observations)
	return results
}

func (s *scan) source(ctx context.Context, source config.Source) Result {
	var repositories []discovery.Repository
	for _, owner := range source.Owners {
		key := catalogKey{credential: source.Credential, owner: strings.ToLower(owner)}
		catalog, ok := s.catalogs[key]
		if !ok {
			catalog.Repositories, catalog.Err = s.owner(ctx, source.Credential, key.owner)
			s.catalogs[key] = catalog
		}
		if catalog.Err != nil {
			return Result{Err: catalog.Err}
		}
		repositories = append(repositories, catalog.Repositories...)
	}
	merged, err := mergeRepositories(repositories)
	return Result{Repositories: merged, Err: err}
}

func validOwners(owners []string) bool {
	if len(owners) == 0 {
		return false
	}
	for _, owner := range owners {
		if !ownerPattern.MatchString(owner) {
			return false
		}
	}
	return true
}

func (s *scan) get(ctx context.Context, name config.CredentialName, path string, app bool) (response, error) {
	if err := ctx.Err(); err != nil {
		return response{}, err
	}
	if strings.HasPrefix(path, "/") {
		path = apiOrigin + path
	}
	key := requestKey{credential: name, url: path, app: app}
	if previous, ok := s.responses[key]; ok {
		return previous.response, previous.err
	}
	var token string
	var err error
	if app {
		token, err = s.client.appJWT(s.client.credentials.entries[name])
	} else {
		token, err = s.client.authorization(ctx, name, "")
	}
	if err != nil {
		return response{}, err
	}
	res, err := s.client.request(ctx, name, http.MethodGet, path, token)
	var upstream *Error
	if !app && s.client.credentials.entries[name].key != nil && errors.As(err, &upstream) && upstream.StatusCode == http.StatusUnauthorized {
		token, err = s.client.authorization(ctx, name, token)
		if err == nil {
			res, err = s.client.request(ctx, name, http.MethodGet, path, token)
		}
	}
	s.responses[key] = cachedResponse{response: res, err: err}
	return res, err
}

func (s *scan) owner(ctx context.Context, name config.CredentialName, owner string) ([]discovery.Repository, error) {
	if s.client.credentials.entries[name].key != nil {
		return s.installationOwner(ctx, name, owner)
	}
	res, err := s.get(ctx, name, "/users/"+url.PathEscape(owner), false)
	if err != nil {
		return nil, err
	}
	var identity struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	}
	if err := decodeJSON(res.body, &identity); err != nil || !strings.EqualFold(identity.Login, owner) {
		return nil, apiError("invalid_owner_metadata", 0, res.requestID)
	}
	var repositories []repositoryDTO
	switch identity.Type {
	case "Organization":
		repositories, err = s.pages(ctx, name, "/orgs/"+owner+"/repos?type=all&per_page=100&page=1", false)
	case "User":
		repositories, err = s.pages(ctx, name, "/users/"+owner+"/repos?type=owner&per_page=100&page=1", false)
		if err == nil {
			err = validateCatalogOwner(repositories, owner)
		}
		if err == nil {
			var accessible []repositoryDTO
			accessible, err = s.pages(ctx, name, "/user/repos?visibility=all&affiliation=owner,collaborator,organization_member&per_page=100&page=1", false)
			repositories = append(repositories, accessible...)
		}
	default:
		err = apiError("unsupported_owner_type", 0, res.requestID)
	}
	if err != nil {
		return nil, err
	}
	if identity.Type == "Organization" {
		if err := validateCatalogOwner(repositories, owner); err != nil {
			return nil, err
		}
	}
	return s.complete(ctx, name, owner, repositories)
}

func (s *scan) installationOwner(ctx context.Context, name config.CredentialName, owner string) ([]discovery.Repository, error) {
	installationID := s.client.credentials.entries[name].installationID
	res, err := s.get(ctx, name, fmt.Sprintf("/app/installations/%d", installationID), true)
	if err != nil {
		return nil, err
	}
	var installation struct {
		ID      int64 `json:"id"`
		Account *struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
		SuspendedAt *time.Time `json:"suspended_at"`
	}
	if err := decodeJSON(res.body, &installation); err != nil || installation.ID != installationID || installation.Account == nil ||
		!strings.EqualFold(installation.Account.Login, owner) || (installation.Account.Type != "Organization" && installation.Account.Type != "User") || installation.SuspendedAt != nil {
		return nil, apiError("invalid_installation_owner", 0, res.requestID)
	}
	repositories, err := s.pages(ctx, name, "/installation/repositories?per_page=100&page=1", true)
	if err != nil {
		return nil, err
	}
	if err := validateCatalogOwner(repositories, owner); err != nil {
		return nil, err
	}
	return s.complete(ctx, name, owner, repositories)
}
