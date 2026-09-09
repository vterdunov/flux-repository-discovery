package github_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	gh "github.com/vterdunov/flux-repository-discovery/internal/github"
)

func TestCredentialsResolveReferencesAndExposeOnlyNames(t *testing.T) {
	const document = `credentials:
  work:
    token:
      valueEnv: WORK_TOKEN
  personal:
    token:
      valueFile: /run/secrets/personal
`
	var reads []string
	credentials, err := gh.LoadCredentials([]byte(document), gh.CredentialOptions{
		LookupEnv: func(name string) (string, bool) { return "github-secret-value", name == "WORK_TOKEN" },
		ReadFile: func(path string) ([]byte, error) {
			reads = append(reads, path)
			return []byte("file-secret-value\n"), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !credentials.Has("work") || !credentials.Has("personal") || credentials.Has("unknown") {
		t.Fatal("registry membership must reflect declared credentials")
	}
	if got, want := credentials.Names(), []config.CredentialName{"personal", "work"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(reads, []string{"/run/secrets/personal"}) {
		t.Fatalf("unexpected file references: %v", reads)
	}
	// Returned names are caller-owned and cannot mutate the registry.
	credentials.Names()[0] = "changed"
	if !credentials.Has("personal") || credentials.Has("changed") {
		t.Fatal("Names leaked mutable registry state")
	}
}

func TestCredentialsRejectAmbiguousOrUnsafeDeclarations(t *testing.T) {
	tests := []struct{ name, document string }{
		{"unknown field", "credentials: {work: {token: {valueEnv: TOKEN, inline: forbidden}}}"},
		{"duplicate name", "credentials:\n  work: {token: {valueEnv: TOKEN}}\n  work: {token: {valueEnv: TOKEN}}\n"},
		{"both token references", "credentials: {work: {token: {valueEnv: TOKEN, valueFile: /secret}}}"},
		{"missing token reference", "credentials: {work: {token: {}}}"},
		{"both auth methods", "credentials: {work: {token: {valueEnv: TOKEN}, githubApp: {appId: 1, installationId: 2, privateKeyEnv: KEY}}}"},
		{"invalid credential name", "credentials: {Bad_Name: {token: {valueEnv: TOKEN}}}"},
		{"missing env", "credentials: {work: {token: {valueEnv: MISSING}}}"},
		{"empty env", "credentials: {work: {token: {valueEnv: EMPTY}}}"},
		{"invalid private key", "credentials: {work: {githubApp: {appId: 1, installationId: 2, privateKeyEnv: TOKEN}}}"},
		{"invalid app id", "credentials: {work: {githubApp: {appId: 0, installationId: 2, privateKeyEnv: KEY}}}"},
		{"invalid installation id", "credentials: {work: {githubApp: {appId: 1, installationId: -1, privateKeyEnv: KEY}}}"},
		{"multiple documents", "credentials: {work: {token: {valueEnv: TOKEN}}}\n---\ncredentials: {}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credentials, err := gh.LoadCredentials([]byte(tt.document), gh.CredentialOptions{
				LookupEnv: func(name string) (string, bool) {
					switch name {
					case "MISSING":
						return "", false
					case "EMPTY":
						return "", true
					default:
						return "never-show-this-secret", true
					}
				},
				ReadFile: func(string) ([]byte, error) { return nil, errors.New("provider leaked never-show-this-secret") },
			})
			if err == nil || credentials != nil {
				t.Fatalf("invalid declaration accepted: credentials=%v err=%v", credentials, err)
			}
			if strings.Contains(err.Error(), "never-show-this-secret") {
				t.Fatal("credential error exposed secret contents")
			}
		})
	}
}

func TestCredentialsFileFailureDoesNotLeakProviderError(t *testing.T) {
	_, err := gh.LoadCredentials([]byte("credentials: {work: {token: {valueFile: /run/secret}}}"), gh.CredentialOptions{
		ReadFile: func(string) ([]byte, error) { return nil, errors.New("read failed with github-secret-value") },
	})
	if err == nil || strings.Contains(err.Error(), "github-secret-value") {
		t.Fatalf("expected sanitized credential error, got %v", err)
	}
}

func tokenCredentials(t *testing.T) *gh.Credentials {
	t.Helper()
	credentials, err := gh.LoadCredentials([]byte("credentials: {token: {token: {valueEnv: TOKEN}}}"), gh.CredentialOptions{
		LookupEnv: func(name string) (string, bool) { return "test-token-do-not-disclose", name == "TOKEN" },
	})
	if err != nil {
		t.Fatal(err)
	}
	return credentials
}
