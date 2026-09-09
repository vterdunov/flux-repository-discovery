package github_test

import (
	"context"
	"net/http"
	"testing"

	gh "github.com/vterdunov/flux-repository-discovery/internal/github"
)

func TestJSONMigrationRequiredRepositoryMetadataStaysFailClosed(t *testing.T) {
	for _, field := range []string{"id", "name", "owner", "archived", "fork", "topics"} {
		for _, form := range []string{"missing", "null"} {
			t.Run(field+" "+form, func(t *testing.T) {
				client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/users/acme" {
						writeJSON(t, w, map[string]string{"login": "acme", "type": "Organization"})
						return
					}
					repo := repository(1, "acme", "repo")
					if form == "missing" {
						delete(repo, field)
					} else {
						repo[field] = nil
					}
					switch r.URL.Path {
					case "/orgs/acme/repos":
						writeJSON(t, w, []any{repo})
					case "/repos/acme/repo":
						// Metadata fallback cannot invent a missing value. Return
						// the same incomplete representation at the detail endpoint.
						writeJSON(t, w, repo)
					default:
						t.Errorf("unexpected request: %s", r.URL)
						w.WriteHeader(http.StatusNotFound)
					}
				}, gh.Options{})
				assertFailedCatalog(t, client.Scan(context.Background(), sources("acme"))["main"])
			})
		}
	}
}

func TestJSONMigrationGitHubAllowsUnrelatedResponseFields(t *testing.T) {
	client := newTokenClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/acme":
			writeJSON(t, w, map[string]any{"login": "acme", "type": "Organization", "node_id": "organization-node", "avatar_url": "https://avatars.githubusercontent.com/u/1"})
		case "/orgs/acme/repos":
			repo := repository(1, "acme", "repo")
			repo["description"] = "An ordinary repository <description> & metadata"
			repo["license"] = map[string]any{"key": "mit", "name": "MIT License", "url": nil}
			repo["permissions"] = map[string]bool{"admin": false, "pull": true}
			repo["owner"] = map[string]any{"login": "acme", "node_id": "owner-node", "site_admin": false}
			writeJSON(t, w, []any{repo})
		default:
			t.Errorf("unexpected request: %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}, gh.Options{})
	result := client.Scan(context.Background(), sources("acme"))["main"]
	if result.Err != nil || len(result.Repositories) != 1 || result.Repositories[0].ID != 1 || result.Repositories[0].Owner != "acme" || result.Repositories[0].Name != "repo" {
		t.Fatalf("ordinary additional GitHub fields invalidated the catalog: %+v", result)
	}
}
