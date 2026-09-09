package github_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	gh "github.com/vterdunov/flux-repository-discovery/internal/github"
)

func appCredentials(t *testing.T) (*gh.Credentials, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	credentials, err := gh.LoadCredentials([]byte(`credentials:
  app:
    githubApp:
      appId: 123
      installationId: 456
      privateKeyEnv: APP_KEY
`), gh.CredentialOptions{LookupEnv: func(name string) (string, bool) { return string(encoded), name == "APP_KEY" }})
	if err != nil {
		t.Fatal(err)
	}
	return credentials, key
}

func appSources(owner string) map[config.SourceName]config.Source {
	return map[config.SourceName]config.Source{"main": {Owners: []string{owner}, Credential: "app"}}
}

func assertAppJWT(t *testing.T, authorization string, key *rsa.PrivateKey, now time.Time) {
	t.Helper()
	if !strings.HasPrefix(authorization, "Bearer ") {
		t.Error("missing JWT bearer authorization")
		return
	}
	parts := strings.Split(strings.TrimPrefix(authorization, "Bearer "), ".")
	if len(parts) != 3 {
		t.Error("application JWT must contain 3 segments")
		return
	}
	var header struct {
		Algorithm string `json:"alg"`
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Error(err)
		return
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Error(err)
		return
	}
	if header.Algorithm != "RS256" {
		t.Errorf("JWT algorithm = %q", header.Algorithm)
	}
	var claims struct {
		IssuedAt  int64           `json:"iat"`
		ExpiresAt int64           `json:"exp"`
		Issuer    json.RawMessage `json:"iss"`
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Error(err)
		return
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Error(err)
		return
	}
	if strings.Trim(string(claims.Issuer), `"`) != "123" {
		t.Errorf("JWT issuer = %s", claims.Issuer)
	}
	if claims.IssuedAt > now.Unix() || claims.IssuedAt < now.Add(-2*time.Minute).Unix() {
		t.Errorf("invalid JWT iat %d at %v", claims.IssuedAt, now)
	}
	if claims.ExpiresAt <= now.Unix() || claims.ExpiresAt > now.Add(10*time.Minute).Unix() {
		t.Errorf("invalid JWT expiry %d at %v", claims.ExpiresAt, now)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Error(err)
		return
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Errorf("invalid RS256 signature: %v", err)
	}
}

func TestGitHubAppJWTInstallationOwnerAndTokenReuse(t *testing.T) {
	credentials, key := appCredentials(t)
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	var tokens atomic.Int32
	client, err := gh.NewClient(credentials, gh.Options{Now: func() time.Time { return now }, HTTPClient: githubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/456":
			assertAppJWT(t, r.Header.Get("Authorization"), key, now)
			writeJSON(t, w, map[string]any{"id": 456, "account": map[string]string{"login": "acme", "type": "Organization"}})
		case "/app/installations/456/access_tokens":
			if r.Method != http.MethodPost {
				t.Errorf("token exchange method = %s", r.Method)
			}
			assertAppJWT(t, r.Header.Get("Authorization"), key, now)
			tokens.Add(1)
			writeJSON(t, w, map[string]any{"token": "installation-secret", "expires_at": now.Add(time.Hour).Format(time.RFC3339)})
		case "/installation/repositories":
			if r.Header.Get("Authorization") != "Bearer installation-secret" {
				t.Error("repository catalog must use installation token")
			}
			writeJSON(t, w, map[string]any{"total_count": 1, "repositories": []any{repository(1, "acme", "private")}})
		default:
			t.Errorf("unexpected App endpoint: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		result := client.Scan(context.Background(), appSources("acme"))["main"]
		if result.Err != nil || len(result.Repositories) != 1 {
			t.Fatalf("App catalog failed: %+v", result)
		}
	}
	if tokens.Load() != 1 {
		t.Fatalf("token exchanges = %d, want 1", tokens.Load())
	}
}

func TestGitHubAppRejectsWrongInstallationEvenWhenCatalogEmpty(t *testing.T) {
	credentials, _ := appCredentials(t)
	client, err := gh.NewClient(credentials, gh.Options{HTTPClient: githubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/456":
			writeJSON(t, w, map[string]any{"id": 456, "account": map[string]string{"login": "other", "type": "User"}})
		case "/app/installations/456/access_tokens":
			writeJSON(t, w, map[string]any{"token": "installation-secret", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
		case "/installation/repositories":
			writeJSON(t, w, map[string]any{"total_count": 0, "repositories": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	assertFailedCatalog(t, client.Scan(context.Background(), appSources("acme"))["main"])
}

func TestGitHubAppConcurrentRefreshUsesOneExchange(t *testing.T) {
	credentials, _ := appCredentials(t)
	var tokens atomic.Int32
	var timeOffset atomic.Int64
	initial := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return initial.Add(time.Duration(timeOffset.Load())) }
	client, err := gh.NewClient(credentials, gh.Options{Now: now, HTTPClient: githubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/456":
			writeJSON(t, w, map[string]any{"id": 456, "account": map[string]string{"login": "acme", "type": "Organization"}})
		case "/app/installations/456/access_tokens":
			sequence := tokens.Add(1)
			writeJSON(t, w, map[string]any{"token": fmt.Sprintf("installation-%d", sequence), "expires_at": now().Add(time.Hour).Format(time.RFC3339)})
		case "/installation/repositories":
			writeJSON(t, w, map[string]any{"total_count": 0, "repositories": []any{}})
		default:
			t.Errorf("unexpected App endpoint: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	scanTogether := func() {
		var workers sync.WaitGroup
		start := make(chan struct{})
		for range 8 {
			workers.Go(func() {
				<-start
				result := client.Scan(context.Background(), appSources("acme"))["main"]
				if result.Err != nil {
					t.Errorf("concurrent scan: %v", result.Err)
				}
			})
		}
		close(start)
		workers.Wait()
	}
	scanTogether()
	if tokens.Load() != 1 {
		t.Fatalf("concurrent first token exchanges = %d, want 1", tokens.Load())
	}
	timeOffset.Store(int64(61 * time.Minute))
	scanTogether()
	if tokens.Load() != 2 {
		t.Fatalf("concurrent refresh exchanges = %d, want 2", tokens.Load())
	}
}

func TestGitHubAppUnauthorizedTokenRefreshesOnce(t *testing.T) {
	credentials, _ := appCredentials(t)
	var tokens atomic.Int32
	client, err := gh.NewClient(credentials, gh.Options{HTTPClient: githubHTTP(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app/installations/456":
			writeJSON(t, w, map[string]any{"id": 456, "account": map[string]string{"login": "acme", "type": "Organization"}})
		case "/app/installations/456/access_tokens":
			sequence := tokens.Add(1)
			writeJSON(t, w, map[string]any{"token": fmt.Sprintf("installation-%d", sequence), "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
		case "/installation/repositories":
			if r.Header.Get("Authorization") == "Bearer installation-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			writeJSON(t, w, map[string]any{"total_count": 0, "repositories": []any{}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	result := client.Scan(context.Background(), appSources("acme"))["main"]
	if result.Err != nil || tokens.Load() != 2 {
		t.Fatalf("token expiry should refresh once: result=%+v exchanges=%d", result, tokens.Load())
	}
}
