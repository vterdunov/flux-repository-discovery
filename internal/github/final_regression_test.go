package github_test

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	gh "github.com/vterdunov/flux-repository-discovery/internal/github"
)

func TestCrossCredentialSkippedObservationInvalidatesConflictingSources(t *testing.T) {
	for _, changedField := range []string{"archived", "fork"} {
		t.Run(changedField, func(t *testing.T) {
			credentials, err := gh.LoadCredentials([]byte(`credentials:
  first: {token: {valueEnv: FIRST}}
  second: {token: {valueEnv: SECOND}}
  independent: {token: {valueEnv: INDEPENDENT}}
`), gh.CredentialOptions{LookupEnv: func(name string) (string, bool) { return "token-" + name, true }})
			if err != nil {
				t.Fatal(err)
			}
			client, err := gh.NewClient(credentials, gh.Options{HTTPClient: githubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/users/acme":
					writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
				case "/users/independent":
					writeJSON(t, w, map[string]string{"login": "independent", "type": "Organization"})
				case "/orgs/acme/repos":
					repo := repository(1, "acme", "repo")
					if r.Header.Get("Authorization") == "Bearer token-SECOND" {
						repo[changedField] = true
						delete(repo, "topics")
					}
					writeJSON(t, w, []any{repo})
				case "/orgs/independent/repos":
					writeJSON(t, w, []any{repository(2, "independent", "repo")})
				default:
					t.Errorf("unexpected metadata request for skipped repository: %s", r.URL)
					w.WriteHeader(404)
				}
			})})
			if err != nil {
				t.Fatal(err)
			}
			result := client.Scan(context.Background(), map[config.SourceName]config.Source{
				"first":       {Owners: []string{"acme"}, Credential: "first"},
				"second":      {Owners: []string{"acme"}, Credential: "second"},
				"independent": {Owners: []string{"independent"}, Credential: "independent"},
			})
			assertFailedCatalog(t, result["first"])
			assertFailedCatalog(t, result["second"])
			if result["independent"].Err != nil || len(result["independent"].Repositories) != 1 {
				t.Fatalf("unrelated source affected by conflict: %+v", result["independent"])
			}
		})
	}
}

func TestCanceledTokenRefreshLeaderDoesNotCancelLiveWaiter(t *testing.T) {
	credentials, _ := appCredentials(t)
	synctest.Test(t, func(t *testing.T) {
		retryStarted := make(chan struct{})
		var exchanges atomic.Int32
		client, err := gh.NewClient(credentials, gh.Options{
			Wait: func(ctx context.Context, _ time.Duration) error {
				close(retryStarted)
				<-ctx.Done()
				return ctx.Err()
			},
			HTTPClient: githubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/app/installations/456":
					writeJSON(t, w, map[string]any{"id": 456, "account": map[string]string{"login": "acme", "type": "Organization"}})
				case "/app/installations/456/access_tokens":
					if exchanges.Add(1) == 1 {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					writeJSON(t, w, map[string]string{"token": "live-token", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
				case "/installation/repositories":
					writeJSON(t, w, map[string]any{"total_count": 0, "repositories": []any{}})
				default:
					t.Errorf("unexpected request: %s", r.URL)
					w.WriteHeader(404)
				}
			}),
		})
		if err != nil {
			t.Fatal(err)
		}
		leaderCtx, cancelLeader := context.WithCancel(context.Background())
		defer cancelLeader()
		leader := make(chan gh.Result, 1)
		go func() { leader <- client.Scan(leaderCtx, appSources("acme"))["main"] }()
		<-retryStarted
		waiterCtx, cancelWaiter := context.WithTimeout(context.Background(), time.Minute)
		defer cancelWaiter()
		waiter := make(chan gh.Result, 1)
		go func() { waiter <- client.Scan(waiterCtx, appSources("acme"))["main"] }()
		// The leader is paused between token-exchange attempts without holding
		// the HTTP gate. The waiter can finish owner lookup and join the active
		// exchange. Wait until both goroutines are durably blocked, then cancel
		// only the leader's context.
		synctest.Wait()
		cancelLeader()
		leaderResult := <-leader
		if !errors.Is(leaderResult.Err, context.Canceled) {
			t.Errorf("leader cancellation lost: %v", leaderResult.Err)
		}
		waiterResult := <-waiter
		if waiterResult.Err != nil {
			t.Fatalf("live waiter inherited another scan's cancellation: %v", waiterResult.Err)
		}
		if exchanges.Load() != 2 {
			t.Fatalf("live waiter must obtain fresh exchange, got %d attempts", exchanges.Load())
		}
	})
}
