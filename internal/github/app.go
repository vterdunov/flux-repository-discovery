package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/vterdunov/flux-repository-discovery/internal/config"
)

type installationToken struct {
	value   string
	expires time.Time
}

type tokenRefresh struct {
	ctx   context.Context
	done  chan struct{}
	token installationToken
	err   error
}

func (c *Client) appJWT(value credential) (string, error) {
	now := c.now()
	claims := struct {
		IssuedAt  int64 `json:"iat"`
		ExpiresAt int64 `json:"exp"`
		Issuer    int64 `json:"iss"`
	}{IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(9 * time.Minute).Unix(), Issuer: value.appID}
	encoded, err := json.Marshal(claims)
	if err != nil {
		return "", apiError("app_authentication_failed", 0, "")
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := header + "." + base64.RawURLEncoding.EncodeToString(encoded)
	digest := sha256.Sum256([]byte(payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, value.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", apiError("app_authentication_failed", 0, "")
	}
	return payload + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// authorization refreshes a token once for all concurrent callers. Waiting for
// another caller's exchange is context-aware and holds no mutex during I/O.
// rejected identifies the precise token which received 401, so late responses
// cannot invalidate a newer token obtained by another scan.
func (c *Client) authorization(ctx context.Context, name config.CredentialName, rejected string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value := c.credentials.entries[name]
	if value.key == nil {
		return value.token, nil
	}
	for {
		state := c.states[name]
		if err := ctx.Err(); err != nil {
			return "", err
		}
		state.mu.Lock()
		if state.token.value != "" && state.token.value != rejected && state.token.expires.After(c.now().Add(time.Minute)) {
			token := state.token.value
			state.mu.Unlock()
			return token, nil
		}
		if active := state.refresh; active != nil {
			state.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-active.done:
				if active.ctx.Err() != nil && (errors.Is(active.err, context.Canceled) || errors.Is(active.err, context.DeadlineExceeded)) {
					// Shared authentication must not import another scan's
					// cancellation. Rejoin or own a new exchange with our context.
					continue
				}
				return active.token.value, active.err
			}
		}
		active := &tokenRefresh{ctx: ctx, done: make(chan struct{})}
		state.refresh = active
		if rejected != "" && state.token.value == rejected {
			state.token = installationToken{}
		}
		state.mu.Unlock()

		active.token, active.err = c.exchange(ctx, name, value)
		state.mu.Lock()
		if active.err == nil {
			state.token = active.token
		}
		state.refresh = nil
		close(active.done)
		state.mu.Unlock()
		return active.token.value, active.err
	}
}

func (c *Client) exchange(ctx context.Context, name config.CredentialName, value credential) (installationToken, error) {
	jwt, err := c.appJWT(value)
	if err != nil {
		return installationToken{}, err
	}
	res, err := c.request(ctx, name, http.MethodPost, fmt.Sprintf("%s/app/installations/%d/access_tokens", apiOrigin, value.installationID), jwt)
	if err != nil {
		return installationToken{}, err
	}
	var token struct {
		Value     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := decodeJSON(res.body, &token); err != nil || !validToken(token.Value) || !token.ExpiresAt.After(c.now().Add(time.Minute)) {
		return installationToken{}, apiError("invalid_installation_token", 0, res.requestID)
	}
	return installationToken{value: token.Value, expires: token.ExpiresAt}, nil
}
