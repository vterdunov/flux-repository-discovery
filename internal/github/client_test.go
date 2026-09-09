package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	gh "github.com/vterdunov/flux-repository-discovery/internal/github"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Run real GitHub requests through httptest without opening local sockets.
func githubHTTP(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	return &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "api.github.com" {
			t.Errorf("attempted request outside GitHub: %s", r.URL.Redacted())
			return nil, errors.New("untrusted host")
		}
		if r.Header.Get("X-GitHub-Api-Version") == "" || r.Header.Get("Accept") == "" {
			t.Error("GitHub requests require a pinned API version and explicit Accept header")
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		response := recorder.Result()
		response.Request = r
		return response, nil
	})}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode fixture: %v", err)
	}
}

func repository(id int, owner, name string) map[string]any {
	return map[string]any{"id": id, "name": name, "full_name": owner + "/" + name,
		"owner": map[string]any{"login": owner}, "archived": false, "fork": false, "topics": []string{"gitops"}}
}

func sources(owners ...string) map[config.SourceName]config.Source {
	return map[config.SourceName]config.Source{"main": {Owners: owners, Credential: "token"}}
}

func newTokenClient(t *testing.T, handler http.HandlerFunc, options gh.Options) *gh.Client {
	t.Helper()
	options.HTTPClient = githubHTTP(t, handler)
	client, err := gh.NewClient(tokenCredentials(t), options)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestPATOrganizationPaginationAndRequestReuse(t *testing.T) {
	var mu sync.Mutex
	counts := make(map[string]int)
	client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token-do-not-disclose" {
			t.Error("PAT authorization missing")
		}
		key := r.URL.Path + "?page=" + r.URL.Query().Get("page")
		mu.Lock()
		counts[key]++
		mu.Unlock()
		switch r.URL.Path {
		case "/users/acme":
			writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
		case "/orgs/acme/repos":
			if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("type") != "all" {
				t.Error("organization list must request all repository visibility with 100 per page")
			}
			if r.URL.Query().Get("page") == "2" {
				writeJSON(t, w, []any{repository(102, "acme", "private")})
				return
			}
			w.Header().Set("Link", `<https://api.github.com/orgs/acme/repos?type=all&per_page=100&page=2>; rel="next"`)
			batch := make([]any, 0, 100)
			for id := 1; id <= 100; id++ {
				batch = append(batch, repository(id, "acme", fmt.Sprintf("repo-%03d", id)))
			}
			writeJSON(t, w, batch)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}, gh.Options{})
	input := sources("acme")
	input["same-owner"] = config.Source{Owners: []string{"acme"}, Credential: "token"}
	result := client.Scan(context.Background(), input)
	for _, name := range []config.SourceName{"main", "same-owner"} {
		if result[name].Err != nil || len(result[name].Repositories) != 101 {
			t.Fatalf("source %s: got %d repositories, err %v", name, len(result[name].Repositories), result[name].Err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(counts) != 3 {
		t.Fatalf("expected owner + two page requests, got %v", counts)
	}
	for path, count := range counts {
		if count != 1 {
			t.Errorf("request %s repeated %d times within one scan", path, count)
		}
	}
}

func TestPATPersonalCatalogIncludesPublicAndAccessiblePrivate(t *testing.T) {
	client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/octocat":
			writeJSON(t, w, map[string]string{"login": "octocat", "type": "User"})
		case "/users/octocat/repos":
			writeJSON(t, w, []any{repository(1, "octocat", "public")})
		case "/user/repos":
			writeJSON(t, w, []any{repository(1, "octocat", "public"), repository(2, "octocat", "private"), repository(3, "outsider", "private")})
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}, gh.Options{})
	result := client.Scan(context.Background(), sources("octocat"))["main"]
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	want := []discovery.Repository{
		{ID: 1, Owner: "octocat", Name: "public", Topics: []string{"gitops"}},
		{ID: 2, Owner: "octocat", Name: "private", Topics: []string{"gitops"}},
	}
	if !reflect.DeepEqual(result.Repositories, want) {
		t.Fatalf("catalog = %+v, want %+v", result.Repositories, want)
	}
}

func TestPATPrivateCatalogErrorsAreNotPublicFallback(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/users/octocat":
					writeJSON(t, w, map[string]string{"login": "octocat", "type": "User"})
				case "/users/octocat/repos":
					writeJSON(t, w, []any{repository(1, "octocat", "public")})
				case "/user/repos":
					w.WriteHeader(status)
					writeJSON(t, w, map[string]string{"message": "upstream echoed test-token-do-not-disclose"})
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}, gh.Options{})
			result := client.Scan(context.Background(), sources("octocat"))["main"]
			assertFailedCatalog(t, result)
			if strings.Contains(result.Err.Error(), "test-token-do-not-disclose") {
				t.Fatal("upstream error exposed token")
			}
		})
	}
}

func TestIncompletePagesNeverReturnPartialCatalogs(t *testing.T) {
	tests := []struct {
		name, secondPage string
		status           int
	}{
		{"denied", `{"message":"denied"}`, 403},
		{"malformed JSON", `[{"id":2`, 200},
		{"wrong envelope", `{"repositories":[]}`, 200},
		{"null list", `null`, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/users/acme" {
					writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
					return
				}
				if r.URL.Query().Get("page") == "2" {
					w.WriteHeader(tt.status)
					if _, err := w.Write([]byte(tt.secondPage)); err != nil {
						t.Error(err)
					}
					return
				}
				w.Header().Set("Link", `<https://api.github.com/orgs/acme/repos?page=2>; rel="next"`)
				writeJSON(t, w, []any{repository(1, "acme", "first")})
			}, gh.Options{})
			assertFailedCatalog(t, client.Scan(context.Background(), sources("acme"))["main"])
		})
	}
}

func TestMetadataCompletionSkipsArchivedAndForks(t *testing.T) {
	client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/acme":
			writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
		case "/orgs/acme/repos":
			active, archived, fork := repository(1, "acme", "active"), repository(2, "acme", "archived"), repository(3, "acme", "fork")
			for _, repo := range []map[string]any{active, archived, fork} {
				delete(repo, "topics")
			}
			archived["archived"], fork["fork"] = true, true
			writeJSON(t, w, []any{active, archived, fork})
		case "/repos/acme/active":
			writeJSON(t, w, repository(1, "acme", "active"))
		default:
			t.Errorf("archived/fork must not trigger metadata requests: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}, gh.Options{})
	result := client.Scan(context.Background(), sources("acme"))["main"]
	if result.Err != nil || !reflect.DeepEqual(result.Repositories, []discovery.Repository{{ID: 1, Owner: "acme", Name: "active", Topics: []string{"gitops"}}}) {
		t.Fatalf("unexpected completed catalog: %+v", result)
	}
}

func TestMalformedMandatoryMetadataFailsClosed(t *testing.T) {
	for _, field := range []string{"id", "name", "owner", "archived", "fork", "topics"} {
		t.Run(field, func(t *testing.T) {
			client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/users/acme" {
					writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
					return
				}
				repo := repository(1, "acme", "bad")
				delete(repo, field)
				if strings.HasPrefix(r.URL.Path, "/repos/") {
					writeJSON(t, w, repo)
					return
				}
				writeJSON(t, w, []any{repo})
			}, gh.Options{})
			assertFailedCatalog(t, client.Scan(context.Background(), sources("acme"))["main"])
		})
	}
}

func TestPaginationRejectsUntrustedHostAndCycles(t *testing.T) {
	for _, link := range []string{
		`<https://evil.example/steal>; rel="next"`,
		`<http://api.github.com/orgs/acme/repos>; rel="next"`,
		`<https://api.github.com/orgs/acme/repos?type=all&per_page=100&page=1>; rel="next"`,
	} {
		t.Run(link, func(t *testing.T) {
			requests := 0
			client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests > 6 {
					w.WriteHeader(403)
					t.Error("pagination loop was not bounded")
					return
				}
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

func TestRedirectCannotSendCredentialsToAnotherHost(t *testing.T) {
	client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/steal", http.StatusFound)
	}, gh.Options{})
	assertFailedCatalog(t, client.Scan(context.Background(), sources("acme"))["main"])
}

func TestSourceIsolationAndUnknownCredentialPreflight(t *testing.T) {
	var requests atomic.Int32
	client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/users/missing":
			w.WriteHeader(http.StatusNotFound)
		case "/users/acme":
			writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
		case "/orgs/acme/repos":
			writeJSON(t, w, []any{})
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}, gh.Options{})
	unknown := client.Scan(context.Background(), map[config.SourceName]config.Source{"unknown": {Owners: []string{"acme"}, Credential: "absent"}})
	assertFailedCatalog(t, unknown["unknown"])
	if requests.Load() != 0 {
		t.Fatal("unknown credential triggered a network request")
	}
	input := sources("acme")
	input["bad"] = config.Source{Owners: []string{"missing"}, Credential: "token"}
	result := client.Scan(context.Background(), input)
	assertFailedCatalog(t, result["bad"])
	if result["main"].Err != nil || len(result["main"].Repositories) != 0 {
		t.Fatalf("independent source failed: %+v", result["main"])
	}
}

func TestTransientRetryAndRateLimitDelay(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var waits []time.Duration
			attempts := 0
			client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/users/acme" {
					writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
					return
				}
				attempts++
				if attempts == 1 {
					if status == http.StatusTooManyRequests {
						w.Header().Set("Retry-After", "2")
					}
					w.WriteHeader(status)
					return
				}
				writeJSON(t, w, []any{})
			}, gh.Options{
				Now: func() time.Time { return time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC) },
				Wait: func(ctx context.Context, delay time.Duration) error {
					waits = append(waits, delay)
					return ctx.Err()
				},
			})
			result := client.Scan(context.Background(), sources("acme"))["main"]
			if result.Err != nil || attempts != 2 {
				t.Fatalf("transient retry result=%+v attempts=%d", result, attempts)
			}
			if len(waits) == 0 || (status == 429 && waits[0] < 2*time.Second) {
				t.Fatalf("retry did not honor delay: %v", waits)
			}
		})
	}
}

func TestCanceledContextStopsBeforeRequest(t *testing.T) {
	client := newTokenClient(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("canceled scan made request")
		w.WriteHeader(500)
	}, gh.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := client.Scan(ctx, sources("acme"))["main"]
	assertFailedCatalog(t, result)
	if !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("cancellation identity lost: %v", result.Err)
	}
}

func TestRateLimitCooldownIsSharedAcrossScans(t *testing.T) {
	for _, rateKind := range []string{"retry-after", "reset"} {
		t.Run(rateKind, func(t *testing.T) {
			now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
			var requests, waits int
			client := newTokenClient(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++
				if rateKind == "retry-after" {
					w.Header().Set("Retry-After", "60")
					w.WriteHeader(http.StatusTooManyRequests)
				} else {
					w.Header().Set("X-RateLimit-Remaining", "0")
					w.Header().Set("X-RateLimit-Reset", fmt.Sprint(now.Add(time.Minute).Unix()))
					w.WriteHeader(http.StatusForbidden)
				}
			}, gh.Options{Now: func() time.Time { return now }, Wait: func(_ context.Context, delay time.Duration) error {
				waits++
				if delay < time.Minute {
					t.Errorf("cooldown shortened to %v", delay)
				}
				return context.DeadlineExceeded
			}})
			for range 2 {
				assertFailedCatalog(t, client.Scan(context.Background(), sources("acme"))["main"])
			}
			if requests != 1 || waits < 2 {
				t.Fatalf("later scan bypassed cooldown: requests=%d waits=%d", requests, waits)
			}
		})
	}
}

func TestRetriesAreBoundedAndTransportCancellationIsPreserved(t *testing.T) {
	t.Run("persistent upstream failure", func(t *testing.T) {
		requests := 0
		client := newTokenClient(t, func(w http.ResponseWriter, _ *http.Request) {
			requests++
			if requests > 5 {
				t.Error("temporary-error retry budget exceeded")
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
		}, gh.Options{Wait: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }})
		assertFailedCatalog(t, client.Scan(context.Background(), sources("acme"))["main"])
		if requests < 2 {
			t.Fatal("temporary error was never retried")
		}
	})
	t.Run("in-flight cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		requests := 0
		client, err := gh.NewClient(tokenCredentials(t), gh.Options{HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			cancel()
			<-r.Context().Done()
			return nil, r.Context().Err()
		})}})
		if err != nil {
			t.Fatal(err)
		}
		result := client.Scan(ctx, sources("acme"))["main"]
		assertFailedCatalog(t, result)
		if !errors.Is(result.Err, context.Canceled) || requests != 1 {
			t.Fatalf("canceled request retried or lost identity: result=%+v requests=%d", result, requests)
		}
	})
}

func assertFailedCatalog(t *testing.T, result gh.Result) {
	t.Helper()
	if result.Err == nil || len(result.Repositories) != 0 {
		t.Fatalf("expected complete failure without partial repositories: %+v", result)
	}
}
