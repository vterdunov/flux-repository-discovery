// Package cli implements the operator command-line contract.
package cli

import (
	"context"
	"io"
	"net/http"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
)

type ServeOptions struct {
	Config          config.Config
	CredentialsFile string
	Env             map[string]string
}
type Options struct {
	Stdout     io.Writer
	Stderr     io.Writer
	LookupEnv  func(string) (string, bool)
	HTTPClient *http.Client
	Version    string
	Serve      func(context.Context, ServeOptions) error
}
