package wpcalc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// Session is a wpcalc /api/v1 client: the generated ClientWithResponses
// (every operation, typed) embedded directly, plus the one thing
// generated code can't give you — carrying a bearer token across calls and
// transparently exchanging it for a new one when it expires, so a caller
// holding a TokenPair from `wpcalc token create` (or POST /tokens) never
// has to notice an access token's one-hour lifetime.
type Session struct {
	*ClientWithResponses

	mu        sync.Mutex
	tokens    TokenPair
	onRefresh func(TokenPair)
	refresher *ClientWithResponses // unauthenticated; only ever calls RefreshTokenWithResponse
}

// Option configures New.
type Option func(*options)

type options struct {
	httpDoer  HttpRequestDoer
	onRefresh func(TokenPair)
}

// WithHTTPDoer supplies the underlying HTTP client (for custom timeouts,
// proxies, or a test server's client). Its Transport, if any, still runs
// on every request — the auto-refresh behavior wraps it, rather than
// replacing it.
func WithHTTPDoer(c HttpRequestDoer) Option {
	return func(o *options) { o.httpDoer = c }
}

// WithOnRefresh registers a callback invoked with the new TokenPair every
// time the session transparently refreshes — the hook a long-running
// process needs to persist the new refresh token before the old one it
// has on disk becomes useless (refresh tokens are single-use).
func WithOnRefresh(fn func(TokenPair)) Option {
	return func(o *options) { o.onRefresh = fn }
}

// New builds a Session authenticated with tokens (typically straight from
// `wpcalc token create`, POST /tokens, or a prior WithOnRefresh callback).
// baseURL should include the /api/v1 prefix, e.g.
// "http://localhost:8080/api/v1".
func New(baseURL string, tokens TokenPair, opts ...Option) (*Session, error) {
	if tokens.AccessToken == "" {
		return nil, errors.New("wpcalc: an access token is required")
	}
	cfg := &options{}
	for _, opt := range opts {
		opt(cfg)
	}
	base := cfg.httpDoer
	if base == nil {
		base = http.DefaultClient
	}

	s := &Session{tokens: tokens, onRefresh: cfg.onRefresh}

	refresher, err := NewClientWithResponses(baseURL, WithHTTPClient(base))
	if err != nil {
		return nil, fmt.Errorf("wpcalc: %w", err)
	}
	s.refresher = refresher

	authed, err := NewClientWithResponses(baseURL, WithHTTPClient(&refreshingDoer{base: base, session: s}))
	if err != nil {
		return nil, fmt.Errorf("wpcalc: %w", err)
	}
	s.ClientWithResponses = authed
	return s, nil
}

// Tokens returns the session's current token pair — the access token most
// recently issued or refreshed, and the refresh token that can still
// exchange for the next one. Read this after a request if you are not
// using WithOnRefresh and want to persist state before the process exits.
func (s *Session) Tokens() TokenPair {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens
}

// refreshingDoer wraps an HttpRequestDoer with transparent, single-flight
// refresh-on-401: on a 401 response, it exchanges the session's current
// refresh token for a new pair and retries the original request exactly
// once. Concurrent callers that all hit 401 at the same moment coalesce
// into a single refresh — refresh tokens are single-use, so a second,
// redundant refresh attempt would itself fail.
type refreshingDoer struct {
	base    HttpRequestDoer
	session *Session
}

func (d *refreshingDoer) Do(req *http.Request) (*http.Response, error) {
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("wpcalc: read request body for retry: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	attemptedWith := d.session.Tokens().AccessToken
	req.Header.Set("Authorization", "Bearer "+attemptedWith)

	resp, err := d.base.Do(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}

	// Buffer (don't just discard) the original 401's body: if the refresh
	// attempt below also fails, this response — with a fresh, readable
	// body — is what gets returned, and a caller reading a body already
	// closed out from under it would see "read on closed response body"
	// instead of the actual 401 payload.
	respBodyBytes, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr == nil {
		resp.Body = io.NopCloser(bytes.NewReader(respBodyBytes))
	}

	newAccessToken, refreshErr := d.session.refresh(req.Context(), attemptedWith)
	if refreshErr != nil {
		// The original 401 is more informative than a refresh failure the
		// caller didn't ask about — surface that, not this one.
		return resp, err
	}

	retry := req.Clone(req.Context())
	if bodyBytes != nil {
		retry.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}
	retry.Header.Set("Authorization", "Bearer "+newAccessToken)
	return d.base.Do(retry)
}

// refresh exchanges the current refresh token for a new pair, unless
// another goroutine already did so since attemptedWith was read — in
// which case it just hands back the token that's current now, with no
// network call (and, critically, without trying to spend an
// already-spent, single-use refresh token).
func (s *Session) refresh(ctx context.Context, attemptedWith string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tokens.AccessToken != attemptedWith {
		return s.tokens.AccessToken, nil
	}

	resp, err := s.refresher.RefreshTokenWithResponse(ctx, RefreshTokenJSONRequestBody{
		RefreshToken: s.tokens.RefreshToken,
	})
	if err != nil {
		return "", fmt.Errorf("wpcalc: refresh: %w", err)
	}
	if resp.JSON201 == nil {
		return "", fmt.Errorf("wpcalc: refresh: status %d", resp.HTTPResponse.StatusCode)
	}

	s.tokens = *resp.JSON201
	if s.onRefresh != nil {
		s.onRefresh(s.tokens)
	}
	return s.tokens.AccessToken, nil
}
