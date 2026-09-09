package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/vterdunov/flux-repository-discovery/internal/app"
	"github.com/vterdunov/flux-repository-discovery/internal/cli"
)

var version = "dev"

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	return cli.Run(ctx, os.Args[1:], cli.Options{
		Version: version,
		Serve: func(ctx context.Context, settings cli.ServeOptions) error {
			return app.Run(ctx, settings, app.Options{})
		},
	})
}
