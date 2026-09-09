// Package httpapi exposes the Flux ExternalService and operator HTTP contracts.
package httpapi

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
	"github.com/vterdunov/flux-repository-discovery/internal/discovery"
	"github.com/vterdunov/flux-repository-discovery/internal/jsonwire"
	"github.com/vterdunov/flux-repository-discovery/internal/service"
)

type Middleware func(http.Handler) http.Handler
type Options struct {
	MaxBodyBytes       int64
	Middleware         []Middleware
	InputsMiddleware   []Middleware
	OperatorMiddleware []Middleware
}

func New(s *service.Service, options Options) http.Handler {
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = config.MaxBytes
	}
	mux := http.NewServeMux()
	mux.Handle("/inputs/{filter}", chain(method(http.MethodGet, func(w http.ResponseWriter, r *http.Request) {
		inputs, err := s.Inputs(config.FilterName(r.PathValue("filter")))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Inputs []discovery.Input `json:"inputs"`
		}{inputs})
	}), options.InputsMiddleware))
	mux.Handle("/api/v1/status", chain(method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, http.StatusOK, s.Status()) }), options.OperatorMiddleware))
	mux.Handle("/api/v1/dry-run", chain(method(http.MethodPost, func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, options.MaxBodyBytes))
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds size limit")
			} else {
				writeError(w, http.StatusBadRequest, "invalid_request", "could not read request body")
			}
			return
		}
		var request struct {
			Config jsontext.Value `json:"config"`
		}
		if err := json.Unmarshal(data, &request, json.RejectUnknownMembers(true)); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "expected a JSON object containing only config")
			return
		}
		if len(request.Config) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "request must contain config")
			return
		}
		candidate, err := config.Decode(request.Config)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid_config", err.Error())
			return
		}
		report, err := s.DryRun(r.Context(), candidate)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, report)
	}), options.OperatorMiddleware))
	mux.Handle("/healthz", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, struct {
			Status string `json:"status"`
		}{"ok"})
	}))
	mux.Handle("/readyz", method(http.MethodGet, func(w http.ResponseWriter, _ *http.Request) {
		if !s.Ready() {
			writeError(w, http.StatusServiceUnavailable, "not_ready", "service scan loop is not running")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Status string `json:"status"`
		}{"ready"})
	}))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "endpoint does not exist")
	})
	handler := chain(mux, options.Middleware)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("HTTP handler panicked")
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		handler.ServeHTTP(w, r)
	})
}

func chain(handler http.Handler, middleware []Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

func method(allowed string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != allowed && (allowed != http.MethodGet || r.Method != http.MethodHead) {
			w.Header().Set("Allow", allowed)
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method is not supported")
			return
		}
		next(w, r)
	})
}

func writeServiceError(w http.ResponseWriter, err error) {
	var problem *service.Error
	if !errors.As(err, &problem) {
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	status := http.StatusServiceUnavailable
	switch problem.Code {
	case "not_found":
		status = http.StatusNotFound
	case "invalid_config":
		status = http.StatusUnprocessableEntity
	case "busy":
		status = http.StatusTooManyRequests
	case "not_ready", "source_unavailable", "stale", "result_limit":
	default:
		status = http.StatusInternalServerError
	}
	writeJSON(w, status, struct {
		Error *service.Error `json:"error"`
	}{problem})
}

func writeError(w http.ResponseWriter, status int, code service.ErrorCode, message string) {
	writeJSON(w, status, struct {
		Error service.Error `json:"error"`
	}{service.Error{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := jsonwire.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		data = []byte(`{"error":{"code":"internal_error","message":"could not encode response"}}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		slog.Debug("HTTP response write failed")
	}
}
