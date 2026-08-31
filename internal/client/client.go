// Copyright IBM Corp. 2021, 2025
// SPDX-License-Identifier: MPL-2.0

// Package client is the hand-written Comlaude API client (ADR-0001). It owns
// authentication, the response envelope, error mapping, and retry policy;
// resource code never sees raw HTTP or envelope internals.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the single production endpoint; the API has no
// documented sandbox.
const DefaultBaseURL = "https://api.comlaude.com"

const requestTimeout = 30 * time.Second

// maxAttempts bounds the retry loop: one initial try plus three retries.
const maxAttempts = 4

const defaultRetryBaseDelay = 250 * time.Millisecond

// Client is a Comlaude API client. Safe for concurrent use.
type Client struct {
	baseURL  string
	username string
	password string
	apiKey   string

	httpClient     *http.Client
	retryBaseDelay time.Duration

	mu    sync.Mutex // guards token; login happens under the lock (single-flight)
	token string
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient replaces the underlying HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithRetryBaseDelay overrides the backoff base delay (tests use ~1ms).
func WithRetryBaseDelay(d time.Duration) Option {
	return func(c *Client) { c.retryBaseDelay = d }
}

// New builds a client. No network call happens until Login or the first
// request.
func New(baseURL, username, password, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:        strings.TrimSuffix(baseURL, "/"),
		username:       username,
		password:       password,
		apiKey:         apiKey,
		httpClient:     &http.Client{Timeout: requestTimeout},
		retryBaseDelay: defaultRetryBaseDelay,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type tokenData struct {
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// Login performs POST /api_login and caches the bearer token for the
// lifetime of the process. Called eagerly at provider Configure.
func (c *Client) Login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.loginLocked(ctx)
}

// ensureToken returns a valid cached token, logging in if none exists yet.
// Concurrent first calls serialize here so only one login happens.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token == "" {
		if err := c.loginLocked(ctx); err != nil {
			return "", err
		}
	}
	return c.token, nil
}

func (c *Client) loginLocked(ctx context.Context) error {
	form := url.Values{
		"username": {c.username},
		"password": {c.password},
		"api_key":  {c.apiKey},
	}
	status, body, err := c.sendWithRetry(ctx, http.MethodPost, "/api_login", form, "")
	if err != nil {
		return err
	}

	var env Envelope[tokenData]
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("decoding login response (HTTP %d): %w", status, err)
	}
	if status == http.StatusUnauthorized {
		return apiError(ErrAuth, status, env.Errors)
	}
	if status != http.StatusOK {
		return mapStatusError(status, body)
	}
	tok, err := env.One()
	if err != nil {
		return fmt.Errorf("login response: %w", err)
	}
	c.token = tok.AccessToken
	return nil
}

// do executes one authenticated request, logging in first if no token is
// cached, and returns the response body for 2xx responses. A 401 mid-run
// (token expired server-side) triggers exactly one re-login and replay;
// other non-2xx responses map to typed errors carrying the envelope's
// messages.
func (c *Client) do(ctx context.Context, method, path string, form url.Values) ([]byte, error) {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return nil, err
	}

	status, raw, err := c.sendWithRetry(ctx, method, path, form, token)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		token, err = c.reloginAfter401(ctx, token)
		if err != nil {
			return nil, err
		}
		status, raw, err = c.sendWithRetry(ctx, method, path, form, token)
		if err != nil {
			return nil, err
		}
	}
	if status >= 200 && status < 300 {
		return raw, nil
	}
	return nil, mapStatusError(status, raw)
}

// sendWithRetry sends the request up to maxAttempts times. What is safe to
// retry depends on the verb: idempotent methods (GET/PUT/DELETE) retry on
// 429, 5xx, and transport errors; POST retries only on 429 or a transport
// error (no response received) — never on 5xx, because a create that reached
// the server may have been processed, and duplicating a record in a live
// zone is worse than a failed apply.
func (c *Client) sendWithRetry(ctx context.Context, method, path string, form url.Values, token string) (int, []byte, error) {
	idempotent := method != http.MethodPost
	var status int
	var raw []byte
	var retryAfter string
	var err error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			if serr := c.sleepBackoff(ctx, attempt, retryAfter); serr != nil {
				return 0, nil, serr
			}
		}
		status, raw, retryAfter, err = c.attempt(ctx, method, path, form, token)
		if err != nil {
			continue // transport error: no response received, safe for any verb
		}
		if status == http.StatusTooManyRequests {
			continue
		}
		if status >= 500 && idempotent {
			continue
		}
		return status, raw, nil
	}
	if err != nil {
		return 0, nil, err
	}
	return status, raw, nil
}

// sleepBackoff waits before a retry: Retry-After when the server sent one,
// otherwise exponential backoff with jitter on the configured base delay.
func (c *Client) sleepBackoff(ctx context.Context, attempt int, retryAfter string) error {
	delay := c.retryBaseDelay << (attempt - 1)
	if secs, err := strconv.Atoi(retryAfter); err == nil && secs >= 0 {
		delay = time.Duration(secs) * time.Second
	} else {
		delay += time.Duration(rand.Int64N(int64(delay) + 1)) // jitter up to 2x
	}
	select {
	case <-time.After(delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// attempt sends the request once with the given token.
func (c *Client) attempt(ctx context.Context, method, path string, form url.Values, token string) (int, []byte, string, error) {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, "", err
	}
	return resp.StatusCode, raw, resp.Header.Get("Retry-After"), nil
}

// reloginAfter401 refreshes the cached token after a request-level 401. If
// another goroutine already replaced the stale token, that newer token is
// used instead of logging in again.
func (c *Client) reloginAfter401(ctx context.Context, stale string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != stale {
		return c.token, nil
	}
	if err := c.loginLocked(ctx); err != nil {
		return "", err
	}
	return c.token, nil
}

// many runs a request and decodes a collection out of the envelope.
func many[T any](ctx context.Context, c *Client, method, path string) ([]T, error) {
	raw, err := c.do(ctx, method, path, nil)
	if err != nil {
		return nil, err
	}
	var env Envelope[T]
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decoding %s %s response: %w", method, path, err)
	}
	return env.Many()
}

// one runs a request and decodes a single object out of the envelope.
func one[T any](ctx context.Context, c *Client, method, path string) (T, error) {
	var zero T
	raw, err := c.do(ctx, method, path, nil)
	if err != nil {
		return zero, err
	}
	var env Envelope[T]
	if err := json.Unmarshal(raw, &env); err != nil {
		return zero, fmt.Errorf("decoding %s %s response: %w", method, path, err)
	}
	return env.One()
}
