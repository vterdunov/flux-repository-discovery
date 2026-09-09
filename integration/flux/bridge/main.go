//go:build fluxcontract

// The bridge runs the real service and HTTP handler in the project module.
// Upstream Flux tests control only its fake GitHub source catalog over stdin;
// they consume the real HTTP response over a loopback connection.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/httpapi"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

type repository struct {
	ID       discovery.RepositoryID `json:"id"`
	Owner    string                 `json:"owner"`
	Name     string                 `json:"name"`
	Topics   []string               `json:"topics"`
	Archived bool                   `json:"archived"`
	Fork     bool                   `json:"fork"`
}

type scanRequest struct {
	Repositories []repository `json:"repositories"`
	Fail         bool         `json:"fail"`
}

type response struct {
	URL       string `json:"url,omitempty"`
	ScanError bool   `json:"scanError"`
}

type scanner struct {
	result github.Result
}

// Requests and Scan run sequentially in the control loop. HTTP reads only the
// immutable generation published by the production service.
func (s *scanner) Scan(_ context.Context, sources map[config.SourceName]config.Source) map[config.SourceName]github.Result {
	results := make(map[config.SourceName]github.Result, len(sources))
	for name := range sources {
		results[name] = s.result
	}
	return results
}

func run(input io.Reader, output io.Writer) error {
	source := &scanner{}
	document := config.Document{
		Version: 1,
		Server:  config.Server{Listen: "127.0.0.1:0"},
		Scan: config.Scan{
			Interval: config.Duration(10 * time.Minute),
			Timeout:  config.Duration(2 * time.Minute),
		},
		Sources: map[config.SourceName]config.Source{
			"company": {Owners: []string{"acme"}, Credential: "test-token"},
		},
		Filters: map[config.FilterName]config.FilterSpec{
			"applications": {
				Sources: []config.SourceName{"company"},
				Include: []config.Rule{{Topics: &config.Topics{All: []string{"gitops"}}}},
			},
		},
	}
	configuration, err := config.Parse(document)
	if err != nil {
		return fmt.Errorf("parse discovery config: %w", err)
	}
	runtime, err := config.Prepare(configuration, nil, []config.CredentialName{"test-token"})
	if err != nil {
		return fmt.Errorf("prepare discovery config: %w", err)
	}
	application, err := service.New(runtime, source, service.Options{})
	if err != nil {
		return fmt.Errorf("construct real discovery service: %w", err)
	}
	server := httptest.NewServer(httpapi.New(application, httpapi.Options{}))
	defer server.Close()
	encoder := json.NewEncoder(output)
	if err := encoder.Encode(response{URL: server.URL}); err != nil {
		return err
	}
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	for {
		var request scanRequest
		if err := decoder.Decode(&request); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("decode test source catalog: %w", err)
		}
		source.result = github.Result{Repositories: make([]discovery.Repository, 0, len(request.Repositories))}
		for _, repo := range request.Repositories {
			source.result.Repositories = append(source.result.Repositories, discovery.Repository{
				ID: repo.ID, Owner: repo.Owner, Name: repo.Name, Topics: repo.Topics,
				Archived: repo.Archived, Fork: repo.Fork,
			})
		}
		if request.Fail {
			source.result.Err = errors.New("simulated GitHub source failure")
		}
		scanErr := application.Scan(context.Background())
		if err := encoder.Encode(response{ScanError: scanErr != nil}); err != nil {
			return err
		}
	}
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
