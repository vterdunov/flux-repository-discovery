package app_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/app"
	"github.com/vterdunov/flux-repository-discovery/internal/cli"
	"github.com/vterdunov/flux-repository-discovery/internal/config"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type pipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func newListener() *pipeListener {
	return &pipeListener{connections: make(chan net.Conn), closed: make(chan struct{})}
}
func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error { l.once.Do(func() { close(l.closed) }); return nil }
func (*pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080} }
func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.connections <- server:
		return client, nil
	case <-ctx.Done():
		_ = client.Close()
		_ = server.Close()
		return nil, ctx.Err()
	case <-l.closed:
		_ = client.Close()
		_ = server.Close()
		return nil, net.ErrClosed
	}
}
func settings(t *testing.T) cli.ServeOptions {
	t.Helper()
	parsed, err := config.Parse(config.Document{Version: 1, Server: config.Server{Listen: ":8080"}, Scan: config.Scan{Interval: config.Duration(10 * time.Minute), Timeout: config.Duration(2 * time.Minute)}, Sources: map[config.SourceName]config.Source{"company": {Owners: []string{"acme"}, Credential: "token"}}, Filters: map[config.FilterName]config.FilterSpec{"apps": {Sources: []config.SourceName{"company"}, Include: []config.Rule{{NameRegex: ".*"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return cli.ServeOptions{Config: parsed, CredentialsFile: "credentials.yaml", Env: map[string]string{"FRD_LISTEN": ":9000"}}
}
func credentialOptions() app.Options {
	return app.Options{ReadFile: func(path string) ([]byte, error) {
		if path != "credentials.yaml" {
			return nil, errors.New("unexpected server file")
		}
		return []byte("credentials: {token: {token: {valueEnv: SERVER_TOKEN}}}"), nil
	}, LookupEnv: func(name string) (string, bool) { return "server-only-secret", name == "SERVER_TOKEN" }}
}
func apiRequest(t *testing.T, client *http.Client, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, "http://discovery.local"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(data)
}

func TestRuntimeInitialScanHTTPAndCancellationOfActivePreview(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		listener := newListener()
		defer func() { _ = listener.Close() }()
		started := make(chan struct{})
		release := make(chan struct{})
		previewEntered := make(chan struct{})
		previewCanceled := make(chan struct{})
		var phaseMu sync.Mutex
		phase := 0
		opts := credentialOptions()
		opts.Listen = func(network, address string) (net.Listener, error) {
			if network != "tcp" || address != ":9000" {
				t.Errorf("listen must use effective config, got %s %s", network, address)
			}
			return listener, nil
		}
		opts.ShutdownTimeout = 10 * time.Second
		opts.GitHubHTTPClient = &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			if r.Header.Get("Authorization") != "Bearer server-only-secret" {
				t.Error("server did not resolve GitHub credential")
			}
			phaseMu.Lock()
			current := phase
			phaseMu.Unlock()
			if r.URL.Path == "/users/acme" {
				if current == 0 {
					close(started)
					select {
					case <-release:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
				}
				if current == 1 {
					close(previewEntered)
					<-r.Context().Done()
					close(previewCanceled)
					return nil, r.Context().Err()
				}
			}
			w := httptest.NewRecorder()
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/users/acme":
				_, _ = io.WriteString(w, `{"login":"acme","type":"Organization"}`)
			case "/orgs/acme/repos":
				_, _ = io.WriteString(w, `[]`)
			default:
				t.Errorf("unexpected GitHub path %s", r.URL.Path)
				w.WriteHeader(404)
			}
			res := w.Result()
			res.Request = r
			return res, nil
		})}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		serveOptions := settings(t)
		go func() { done <- app.Run(ctx, serveOptions, opts) }()
		select {
		case <-started:
		case err := <-done:
			t.Fatalf("runtime exited before first scan: %v", err)
		}
		apiTransport := &http.Transport{DialContext: listener.dial}
		defer apiTransport.CloseIdleConnections()
		apiClient := &http.Client{Transport: apiTransport}
		if code, body := apiRequest(t, apiClient, http.MethodGet, "/healthz", ""); code != 200 {
			t.Fatalf("liveness=%d %s", code, body)
		}
		if code, body := apiRequest(t, apiClient, http.MethodGet, "/readyz", ""); code != 200 {
			t.Fatalf("running loop readiness=%d %s", code, body)
		}
		if code, body := apiRequest(t, apiClient, http.MethodGet, "/inputs/apps", ""); code != 503 || strings.Contains(body, `"inputs"`) {
			t.Fatalf("first scan exposed success: %d %s", code, body)
		}
		close(release)
		synctest.Wait()
		if code, body := apiRequest(t, apiClient, http.MethodGet, "/inputs/apps", ""); code != 200 || strings.TrimSpace(body) != `{"inputs":[]}` {
			t.Fatalf("complete empty catalog=%d %s", code, body)
		}
		phaseMu.Lock()
		phase = 1
		phaseMu.Unlock()
		previewDone := make(chan int, 1)
		go func() {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://discovery.local/api/v1/dry-run", strings.NewReader(`{"config":{"version":1,"sources":{"company":{"owners":["acme"],"credential":"token"}},"filters":{"apps":{"sources":["company"],"include":[{"nameRegex":".*"}]}}}}`))
			if err != nil {
				previewDone <- 0
				return
			}
			res, err := apiClient.Do(req)
			if err != nil {
				previewDone <- 0
				return
			}
			_, _ = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			previewDone <- res.StatusCode
		}()
		select {
		case <-previewEntered:
		case code := <-previewDone:
			t.Fatalf("preview ended before upstream request: %d", code)
		case err := <-done:
			t.Fatalf("runtime exited unexpectedly: %v", err)
		}
		before := time.Now()
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("normal runtime cancellation failed: %v", err)
		}
		select {
		case <-previewCanceled:
		default:
			t.Fatal("shutdown did not cancel in-flight GitHub preview")
		}
		if elapsed := time.Since(before); elapsed >= opts.ShutdownTimeout {
			t.Fatalf("graceful shutdown exhausted deadline: %s", elapsed)
		}
		if code := <-previewDone; code == 200 {
			t.Fatal("canceled preview reported success")
		}
	})
}

func TestListenFailureDoesNotStartScanning(t *testing.T) {
	opts := credentialOptions()
	opts.Listen = func(string, string) (net.Listener, error) { return nil, errors.New("address already in use") }
	opts.GitHubHTTPClient = &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		t.Error("scanning started before listener bound")
		return nil, errors.New("unexpected network")
	})}
	err := app.Run(context.Background(), settings(t), opts)
	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("listen failure not reported: %v", err)
	}
}

func TestInvalidServerCredentialsFailBeforeBindingAndDoNotLeakSecrets(t *testing.T) {
	opts := credentialOptions()
	opts.ReadFile = func(string) ([]byte, error) {
		return []byte("credentials: {token: {token: {value: accidentally-inlined-secret}}}"), nil
	}
	opts.Listen = func(string, string) (net.Listener, error) {
		t.Error("invalid credentials bound listener")
		return nil, errors.New("unexpected bind")
	}
	err := app.Run(context.Background(), settings(t), opts)
	if err == nil || strings.Contains(err.Error(), "accidentally-inlined-secret") {
		t.Fatalf("credentials failure missing or secret leaked: %v", err)
	}
}
