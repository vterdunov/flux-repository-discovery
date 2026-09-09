// Package app wires the server runtime and owns its HTTP lifecycle.
package app

import (
	"net"
	"net/http"
	"time"
)

type Options struct {
	ReadFile         func(string) ([]byte, error)
	LookupEnv        func(string) (string, bool)
	GitHubHTTPClient *http.Client
	Listen           func(network, address string) (net.Listener, error)
	ShutdownTimeout  time.Duration
}
