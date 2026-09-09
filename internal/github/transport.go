package github

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
)

const (
	apiOrigin       = "https://api.github.com"
	apiVersion      = "2026-03-10"
	requestTimeout  = 30 * time.Second
	maxResponseSize = 16 << 20
	maxAttempts     = 3
)

// ErrorCode classifies safe errors without exposing an upstream response.
type ErrorCode string

// Error contains only trusted classification and bounded GitHub request IDs.
// Response bodies, transport errors, credentials and request URLs are excluded.
type Error struct {
	Code       ErrorCode
	StatusCode int
	RequestID  string
}

func (e *Error) Error() string {
	message := "GitHub: " + string(e.Code)
	if e.StatusCode != 0 {
		message += fmt.Sprintf(" (HTTP %d)", e.StatusCode)
	}
	if e.RequestID != "" {
		message += " request_id=" + e.RequestID
	}
	return message
}

func apiError(code ErrorCode, status int, requestID string) error {
	return &Error{Code: code, StatusCode: status, RequestID: safeRequestID(requestID)}
}

func safeRequestID(value string) string {
	if len(value) > 128 {
		return ""
	}
	for _, r := range value {
		if !strings.ContainsRune("0123456789abcdefABCDEF:-", r) {
			return ""
		}
	}
	return value
}

type credentialState struct {
	gate     chan struct{}
	mu       sync.Mutex
	cooldown time.Time
	token    installationToken
	refresh  *tokenRefresh
}

type response struct {
	body      []byte
	link      string
	requestID string
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          20,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
	}
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func trustedURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "api.github.com" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return nil, apiError("untrusted_api_url", 0, "")
	}
	return u, nil
}

// request serializes each credential's actual HTTP attempts. Both background
// discovery and dry-run therefore observe the same cooldown and cancellation.
func (c *Client) request(ctx context.Context, name config.CredentialName, method, rawURL, authorization string) (response, error) {
	if _, err := trustedURL(rawURL); err != nil {
		return response{}, err
	}
	state := c.states[name]
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return response{}, err
		}
		select {
		case <-ctx.Done():
			return response{}, ctx.Err()
		case state.gate <- struct{}{}:
		}
		result, retry, err := c.attempt(ctx, state, method, rawURL, authorization)
		<-state.gate
		if err == nil || !retry || attempt == maxAttempts-1 {
			return result, err
		}
		// Rate limits wait at the next attempt's shared gate. Other temporary
		// errors get a bounded exponential delay and release the gate meanwhile.
		state.mu.Lock()
		limited := state.cooldown.After(c.now())
		state.mu.Unlock()
		if !limited {
			if err := c.wait(ctx, 250*time.Millisecond*time.Duration(1<<attempt)); err != nil {
				return response{}, safeContextError(ctx, err)
			}
		}
	}
	panic("unreachable retry loop")
}

func (c *Client) attempt(ctx context.Context, state *credentialState, method, rawURL, authorization string) (response, bool, error) {
	state.mu.Lock()
	until := state.cooldown
	state.mu.Unlock()
	if delay := until.Sub(c.now()); delay > 0 {
		if err := c.wait(ctx, delay); err != nil {
			return response{}, false, safeContextError(ctx, err)
		}
		state.mu.Lock()
		if state.cooldown.Equal(until) {
			state.cooldown = time.Time{}
		}
		state.mu.Unlock()
	}
	if err := ctx.Err(); err != nil {
		return response{}, false, err
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return response{}, false, apiError("invalid_api_request", 0, "")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("Authorization", "Bearer "+authorization)
	req.Header.Set("User-Agent", "flux-repository-discovery")
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return response{}, false, safeContextError(ctx, err)
		}
		return response{}, true, apiError("transport_failure", 0, "")
	}
	defer func() { _ = res.Body.Close() }()
	body, readErr := io.ReadAll(io.LimitReader(res.Body, maxResponseSize+1))
	requestID := safeRequestID(res.Header.Get("X-GitHub-Request-Id"))
	if readErr != nil {
		if ctx.Err() != nil {
			return response{}, false, ctx.Err()
		}
		return response{}, true, apiError("incomplete_response", res.StatusCode, requestID)
	}
	if len(body) > maxResponseSize {
		return response{}, false, apiError("response_too_large", res.StatusCode, requestID)
	}
	delay, limited := rateLimitDelay(res, body, c.now())
	if limited {
		state.mu.Lock()
		if until := c.now().Add(delay); until.After(state.cooldown) {
			state.cooldown = until
		}
		state.mu.Unlock()
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		code := ErrorCode("upstream_failure")
		if limited {
			code = "rate_limited"
		} else if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
			code = "access_denied"
		}
		retry := limited || res.StatusCode >= 500
		return response{}, retry, apiError(code, res.StatusCode, requestID)
	}
	return response{body: body, link: strings.Join(res.Header.Values("Link"), ","), requestID: requestID}, false, nil
}

func safeContextError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return apiError("request_interrupted", 0, "")
}

func rateLimitDelay(res *http.Response, body []byte, now time.Time) (time.Duration, bool) {
	var delay time.Duration
	if value := res.Header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.ParseInt(value, 10, 32); err == nil && seconds >= 0 {
			delay = time.Duration(seconds) * time.Second
		} else if until, err := http.ParseTime(value); err == nil {
			delay = until.Sub(now)
		}
	}
	remaining := res.Header.Get("X-RateLimit-Remaining")
	if remaining == "0" {
		if seconds, err := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			delay = max(delay, time.Unix(seconds, 0).Sub(now))
		}
	}
	limited := res.StatusCode == http.StatusTooManyRequests || remaining == "0" || (res.StatusCode == http.StatusForbidden && res.Header.Get("Retry-After") != "")
	if res.StatusCode == http.StatusForbidden && !limited {
		var detail struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &detail) == nil && strings.Contains(strings.ToLower(detail.Message), "secondary rate limit") {
			limited = true
		}
	}
	if limited && delay <= 0 {
		delay = time.Minute
	}
	return delay, limited
}
