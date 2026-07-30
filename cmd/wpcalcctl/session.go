package main

import (
	"fmt"

	wpcalc "github.com/rolsim/wpcalc/sdk/go"
)

// newSession loads stored credentials and builds a Session that persists
// any auto-refresh back to disk immediately — refresh tokens are
// single-use, so if this process exits without saving a refresh that just
// happened, the next invocation would present an already-spent token and
// fail entirely.
func newSession() (*wpcalc.Session, error) {
	creds, err := loadCredentials()
	if err != nil {
		return nil, err
	}
	sess, err := wpcalc.New(creds.Server, creds.Tokens, wpcalc.WithOnRefresh(func(p wpcalc.TokenPair) {
		creds.Tokens = p
		// Best-effort: a failed save here must not fail the request that
		// triggered it. Worst case, the next command re-logs-in.
		_ = saveCredentials(creds)
	}))
	if err != nil {
		return nil, fmt.Errorf("wpcalcctl: %w", err)
	}
	return sess, nil
}

// apiError turns a failed response's typed default-error body (present on
// every non-2xx response the generated client parsed) into a Go error.
// jsonDefault is nil only when the server sent a status code with no JSON
// body at all — a transport-level oddity, not a normal API failure.
func apiError(action string, status int, body []byte, jsonDefault *wpcalc.Error) error {
	if jsonDefault != nil {
		msg := jsonDefault.Error
		if jsonDefault.Message != nil && *jsonDefault.Message != "" {
			msg = msg + ": " + *jsonDefault.Message
		}
		return fmt.Errorf("%s: %s (status %d)", action, msg, status)
	}
	return fmt.Errorf("%s: status %d: %s", action, status, string(body))
}
