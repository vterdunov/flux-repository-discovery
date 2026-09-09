package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/cli"
	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
	"github.com/vterdunov/flux-repository-discovery/internal/httpapi"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

// Run validates all startup inputs before binding, then owns both the scan loop
// and HTTP server until cancellation or a listener failure. Normal shutdown is
// successful; no goroutines or handlers are intentionally left in the background.
func Run(ctx context.Context, settings cli.ServeOptions, options Options) error {
	if err := ctx.Err(); err != nil {
		return nil
	}
	if options.ShutdownTimeout == 0 {
		options.ShutdownTimeout = 10 * time.Second
	}
	if options.ShutdownTimeout < 0 {
		return errors.New("shutdown timeout must be positive")
	}
	if options.ReadFile == nil {
		options.ReadFile = readBoundedFile
	}
	readSecret := func(path string) ([]byte, error) {
		data, err := options.ReadFile(path)
		if err != nil || len(data) > config.MaxBytes {
			return nil, errors.New("could not read bounded credential file")
		}
		return data, nil
	}
	data, err := readSecret(settings.CredentialsFile)
	if err != nil {
		return err
	}
	credentials, err := github.LoadCredentials(data, github.CredentialOptions{ReadFile: readSecret, LookupEnv: options.LookupEnv})
	if err != nil {
		return err
	}
	githubClient, err := github.NewClient(credentials, github.Options{HTTPClient: options.GitHubHTTPClient})
	if err != nil {
		return err
	}
	runtime, err := config.Prepare(settings.Config, settings.Env, credentials.Names())
	if err != nil {
		return err
	}
	discovery, err := service.New(runtime, githubClient, service.Options{})
	if err != nil {
		return err
	}
	effective := runtime.Effective()
	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if options.Listen == nil {
		options.Listen = func(network, address string) (net.Listener, error) {
			return (&net.ListenConfig{}).Listen(runtimeCtx, network, address)
		}
	}
	listener, err := options.Listen("tcp", effective.Server().Listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer func() { _ = listener.Close() }()
	writeTimeout := time.Duration(effective.Scan().Timeout)
	if writeTimeout > math.MaxInt64-30*time.Second {
		writeTimeout = time.Duration(math.MaxInt64)
	} else {
		writeTimeout += 30 * time.Second
	}
	server := &http.Server{
		Handler:           httpapi.New(discovery, httpapi.Options{}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 * 1024,
		BaseContext:       func(net.Listener) context.Context { return runtimeCtx },
	}
	httpDone := make(chan error, 1)
	scanDone := make(chan error, 1)
	go func() { httpDone <- server.Serve(listener) }()
	go func() { scanDone <- discovery.Run(runtimeCtx) }()
	slog.Info("discovery server started", "address", listener.Addr().String())

	var cause error
	var httpEnded, scanEnded bool
	select {
	case <-ctx.Done():
	case cause = <-httpDone:
		httpEnded = true
	case cause = <-scanDone:
		scanEnded = true
	}
	cancel() // Cancels both the background scan and every active request.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), options.ShutdownTimeout)
	defer shutdownCancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		_ = server.Close()
	}
	if !httpEnded {
		err := <-httpDone
		if cause == nil && !errors.Is(err, http.ErrServerClosed) {
			cause = err
		}
	}
	if !scanEnded {
		err := <-scanDone
		if cause == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			cause = err
		}
	}
	slog.Info("discovery server stopped")
	if errors.Is(cause, http.ErrServerClosed) || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		cause = nil
	}
	return errors.Join(cause, shutdownErr)
}

func readBoundedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(io.LimitReader(file, config.MaxBytes+1))
}
