package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	gh "github.com/vterdunov/flux-repository-discovery/internal/github"
)

func TestJSONFieldAliasesCannotOverrideRepositoryIdentity(t *testing.T) {
	for _, alias := range []string{`"id":1,"id":2`, `"id":1,"ID":2`} {
		t.Run(alias, func(t *testing.T) {
			client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/users/acme" {
					writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
					return
				}
				payload := `[{` + alias + `,"name":"repo","owner":{"login":"acme"},"archived":false,"fork":false,"topics":[]}]`
				if _, err := w.Write([]byte(payload)); err != nil {
					t.Error(err)
				}
			}, gh.Options{})
			assertFailedCatalog(t, client.Scan(context.Background(), sources("acme"))["main"])
		})
	}
}

func TestSkippedRepositoryCannotHideConflictingDuplicateID(t *testing.T) {
	for _, changedField := range []string{"archived", "fork", "name"} {
		t.Run(changedField, func(t *testing.T) {
			client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/users/octocat":
					writeJSON(t, w, map[string]string{"login": "octocat", "type": "User"})
				case "/users/octocat/repos":
					writeJSON(t, w, []any{repository(1, "octocat", "repo")})
				case "/user/repos":
					changed := repository(1, "octocat", "repo")
					if changedField == "name" {
						changed = repository(1, "octocat", "renamed")
					} else {
						changed[changedField] = true
					}
					writeJSON(t, w, []any{changed})
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(404)
				}
			}, gh.Options{})
			assertFailedCatalog(t, client.Scan(context.Background(), sources("octocat"))["main"])
		})
	}
}

func TestScopedOrganizationCatalogCannotSilentlyDiscardForeignOwner(t *testing.T) {
	client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/acme" {
			writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
			return
		}
		writeJSON(t, w, []any{repository(1, "foreign", "repo")})
	}, gh.Options{})
	assertFailedCatalog(t, client.Scan(context.Background(), sources("acme"))["main"])
}

func TestPaginationCannotNarrowVisibilityOrSkipPages(t *testing.T) {
	for _, next := range []string{
		"https://api.github.com/orgs/acme/repos?type=public&per_page=100&page=2",
		"https://api.github.com/orgs/acme/repos?type=all&per_page=20&page=2",
		"https://api.github.com/orgs/acme/repos?type=all&per_page=100&page=3",
	} {
		t.Run(next, func(t *testing.T) {
			client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/users/acme" {
					writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
					return
				}
				if r.URL.Query().Get("page") == "1" {
					w.Header().Set("Link", "<"+next+">; rel=\"next\"")
					writeJSON(t, w, []any{repository(1, "acme", "repo")})
					return
				}
				writeJSON(t, w, []any{})
			}, gh.Options{})
			assertFailedCatalog(t, client.Scan(context.Background(), sources("acme"))["main"])
		})
	}
}

func TestMalformedPaginationCannotBecomeSuccessfulEndOfList(t *testing.T) {
	for _, link := range []string{
		`<https://api.github.com/orgs/acme/repos?page=2>; rel="next`,
		`https://api.github.com/orgs/acme/repos?page=2; rel="next"`,
		`<https://api.github.com/orgs/acme/repos?page=2>`,
		`<https://api.github.com/orgs/acme/repos?page=2>; rel="next", <https://api.github.com/orgs/acme/repos?page=3>; rel="next"`,
	} {
		t.Run(link, func(t *testing.T) {
			client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/users/acme" {
					writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
					return
				}
				w.Header().Set("Link", link)
				writeJSON(t, w, []any{repository(1, "acme", "repo")})
			}, gh.Options{})
			assertFailedCatalog(t, client.Scan(context.Background(), sources("acme"))["main"])
		})
	}
}

func TestExtremeRateResetCannotOverflowWaitDuration(t *testing.T) {
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	waits := 0
	client := newTokenClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "253402300799")
		w.WriteHeader(http.StatusForbidden)
	}, gh.Options{Now: func() time.Time { return now }, Wait: func(_ context.Context, delay time.Duration) error {
		waits++
		if delay <= 0 {
			t.Errorf("positive far-future cooldown overflowed to %v", delay)
		}
		return context.DeadlineExceeded
	}})
	result := client.Scan(context.Background(), sources("acme"))["main"]
	assertFailedCatalog(t, result)
	if waits != 1 || !errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("unexpected cancellation: waits=%d err=%v", waits, result.Err)
	}
}

func TestSlowCredentialCannotConsumeHealthySourceDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		credentials, err := gh.LoadCredentials([]byte(`credentials:
  slow: {token: {valueEnv: SLOW}}
  healthy: {token: {valueEnv: HEALTHY}}
`), gh.CredentialOptions{LookupEnv: func(name string) (string, bool) { return "token-" + name, true }})
		if err != nil {
			t.Fatal(err)
		}
		client, err := gh.NewClient(credentials, gh.Options{HTTPClient: githubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/users/slow":
				<-r.Context().Done()
				w.WriteHeader(http.StatusGatewayTimeout)
			case "/users/healthy":
				writeJSON(t, w, map[string]string{"login": "healthy", "type": "Organization"})
			case "/orgs/healthy/repos":
				writeJSON(t, w, []any{repository(2, "healthy", "repo")})
			default:
				t.Errorf("unexpected request: %s", r.URL)
				w.WriteHeader(404)
			}
		})})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result := client.Scan(ctx, map[config.SourceName]config.Source{
			"a-slow":    {Owners: []string{"slow"}, Credential: "slow"},
			"z-healthy": {Owners: []string{"healthy"}, Credential: "healthy"},
		})
		assertFailedCatalog(t, result["a-slow"])
		if result["z-healthy"].Err != nil || len(result["z-healthy"].Repositories) != 1 {
			t.Fatalf("independent healthy source lost its scan budget: %+v", result["z-healthy"])
		}
	})
}

func TestMalformedAppCatalogAndTokenResponsesFailClosed(t *testing.T) {
	credentials, _ := appCredentials(t)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct{ name, token, catalog string }{
		{"missing total", "", `{"repositories":[]}`},
		{"incomplete total", "", `{"total_count":1,"repositories":[]}`},
		{"null repositories", "", `{"total_count":0,"repositories":null}`},
		{"empty token", `{"token":"","expires_at":"2026-09-09T13:00:00Z"}`, ""},
		{"missing expiry", `{"token":"secret"}`, ""},
		{"expired token", `{"token":"secret","expires_at":"2026-09-09T11:00:00Z"}`, ""},
		{"malformed token JSON", `{"token":"secret"`, ""},
		{"foreign repository", "", `{"total_count":1,"repositories":[{"id":1,"owner":{"login":"other"},"name":"repo","archived":false,"fork":false,"topics":[]}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := gh.NewClient(credentials, gh.Options{Now: func() time.Time { return now }, HTTPClient: githubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/app/installations/456":
					writeJSON(t, w, map[string]any{"id": 456, "account": map[string]string{"login": "acme", "type": "Organization"}})
				case "/app/installations/456/access_tokens":
					if tt.token != "" {
						if _, err := w.Write([]byte(tt.token)); err != nil {
							t.Error(err)
						}
						return
					}
					writeJSON(t, w, map[string]string{"token": "secret", "expires_at": now.Add(time.Hour).Format(time.RFC3339)})
				case "/installation/repositories":
					if tt.catalog != "" {
						if _, err := w.Write([]byte(tt.catalog)); err != nil {
							t.Error(err)
						}
						return
					}
					writeJSON(t, w, map[string]any{"total_count": 0, "repositories": []any{}})
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(404)
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			assertFailedCatalog(t, client.Scan(context.Background(), appSources("acme"))["main"])
		})
	}
}

func TestGitHubResponseFailureDoesNotExposeUntrustedRequestID(t *testing.T) {
	client := newTokenClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-GitHub-Request-Id", "test-token-do-not-disclose")
		w.WriteHeader(http.StatusForbidden)
		writeJSON(t, w, map[string]string{"message": "test-token-do-not-disclose"})
	}, gh.Options{})
	result := client.Scan(context.Background(), sources("acme"))["main"]
	assertFailedCatalog(t, result)
	encoded, err := json.Marshal(result.Err)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(result.Err)+string(encoded), "test-token-do-not-disclose") {
		t.Fatal("untrusted error metadata disclosed a secret")
	}
}

func TestCanceledAppRefreshWaitDoesNotCancelAnotherScan(t *testing.T) {
	credentials, _ := appCredentials(t)
	synctest.Test(t, func(t *testing.T) {
		exchangeStarted := make(chan struct{})
		releaseExchange := make(chan struct{})
		client, err := gh.NewClient(credentials, gh.Options{HTTPClient: githubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/app/installations/456":
				writeJSON(t, w, map[string]any{"id": 456, "account": map[string]string{"login": "acme", "type": "Organization"}})
			case "/app/installations/456/access_tokens":
				close(exchangeStarted)
				<-releaseExchange
				writeJSON(t, w, map[string]string{"token": "secret", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
			case "/installation/repositories":
				writeJSON(t, w, map[string]any{"total_count": 0, "repositories": []any{}})
			default:
				t.Errorf("unexpected request: %s", r.URL)
				w.WriteHeader(404)
			}
		})})
		if err != nil {
			t.Fatal(err)
		}
		background := make(chan gh.Result, 1)
		go func() { background <- client.Scan(context.Background(), appSources("acme"))["main"] }()
		<-exchangeStarted
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result := client.Scan(ctx, appSources("acme"))["main"]
		assertFailedCatalog(t, result)
		if !errors.Is(result.Err, context.DeadlineExceeded) {
			t.Errorf("waiting scan did not retain deadline error: %v", result.Err)
		}
		close(releaseExchange)
		if result := <-background; result.Err != nil {
			t.Fatalf("canceling waiter broke independent scan: %v", result.Err)
		}
	})
}
