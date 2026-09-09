package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/github"
)

// Duration describes the whole scan attempt: Scanner returns complete catalogs,
// not individual source timings. Never log arbitrary Scanner error strings,
// which can wrap credentials, URLs or upstream response bodies.
func logGeneration(ctx context.Context, current *generation, results map[config.SourceName]github.Result, duration time.Duration) {
	for _, name := range keys(current.sources) {
		status := current.sources[name]
		attrs := []slog.Attr{slog.String("source", string(name)), slog.Uint64("generation", current.number), slog.Duration("duration", duration)}
		level := slog.LevelInfo
		if status.Ready {
			attrs = append(attrs, slog.Int("count", len(results[name].Repositories)))
		} else {
			level = slog.LevelWarn
			code := string(status.ErrorCode)
			var upstream *github.Error
			if errors.As(results[name].Err, &upstream) && upstream != nil {
				code = string(upstream.Code)
				attrs = append(attrs, slog.Int("github_status", upstream.StatusCode), slog.String("github_request_id", upstream.RequestID))
			}
			attrs = append(attrs, slog.String("error_code", code))
		}
		slog.LogAttrs(ctx, level, "source scan result", attrs...)
	}
	for _, name := range keys(current.filters) {
		state := current.filters[name]
		attrs := []slog.Attr{slog.String("filter", string(name)), slog.Uint64("generation", current.number)}
		level := slog.LevelInfo
		if state.err == nil {
			attrs = append(attrs, slog.Int("count", len(state.result.Inputs)))
		} else {
			level = slog.LevelWarn
			attrs = append(attrs, slog.String("error_code", string(state.err.Code)))
		}
		slog.LogAttrs(ctx, level, "filter publication result", attrs...)
	}
}
