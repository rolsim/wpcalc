package wpcalc_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/httpx"
	"github.com/rolsim/wpcalc/internal/i18n"
	"github.com/rolsim/wpcalc/internal/store"

	wpcalc "github.com/rolsim/wpcalc/sdk/go"
)

// testServer spins up a real wpcalc server — real SQLite file, real
// migrations, real handler tree — the same way cmd/wpcalc/serve.go does,
// so the SDK is tested against the actual server rather than a stub.
func testServer(t *testing.T) (*httptest.Server, *store.DB) {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "wpcalc.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	bundle, err := i18n.New()
	if err != nil {
		t.Fatalf("i18n.New: %v", err)
	}

	srv, err := httpx.New(httpx.Config{
		DB:     db,
		Bundle: bundle,
		Auth:   auth.NewAccounts(db),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, db
}

// superAdminTokens creates a fresh account, grants it super_admin, and
// mints a real token pair for it — end to end through the same store code
// path `wpcalc token create` uses.
func superAdminTokens(t *testing.T, db *store.DB) wpcalc.TokenPair {
	t.Helper()
	ctx := t.Context()
	uid, err := db.CreateUserWeak(ctx, "sdk-admin", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}
	if err := db.GrantUserRole(ctx, uid, nil, nil, "super_admin"); err != nil {
		t.Fatalf("GrantUserRole: %v", err)
	}
	accessToken, accessID, accessExpiry, err := db.CreateAPIToken(ctx, uid, "sdk-test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	refreshToken, _, refreshExpiry, err := db.CreateRefreshToken(ctx, uid, "sdk-test")
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	return wpcalc.TokenPair{
		AccessTokenId:         accessID,
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessExpiry,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshExpiry,
		Name:                  "sdk-test",
	}
}

func TestSessionListsTenants(t *testing.T) {
	ts, db := testServer(t)
	tokens := superAdminTokens(t, db)

	sess, err := wpcalc.New(ts.URL+"/api/v1", tokens)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := sess.ListTenantsWithResponse(t.Context())
	if err != nil {
		t.Fatalf("ListTenantsWithResponse: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode(), resp.Body)
	}
	if resp.JSON200 == nil || len(*resp.JSON200) == 0 {
		t.Fatalf("tenants = %v", resp.JSON200)
	}
	if (*resp.JSON200)[0].Name != "Default" {
		t.Fatalf("tenants[0] = %+v", (*resp.JSON200)[0])
	}
}

func TestSessionRejectsAnInvalidToken(t *testing.T) {
	ts, _ := testServer(t)
	sess, err := wpcalc.New(ts.URL+"/api/v1", wpcalc.TokenPair{AccessToken: "wpat_not-real", RefreshToken: "wprt_not-real"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	resp, err := sess.ListTenantsWithResponse(t.Context())
	if err != nil {
		t.Fatalf("ListTenantsWithResponse: %v", err)
	}
	if resp.StatusCode() != 401 {
		t.Fatalf("status %d, want 401: %s", resp.StatusCode(), resp.Body)
	}
}

func TestSessionAutoRefreshesAnExpiredAccessToken(t *testing.T) {
	ts, db := testServer(t)
	tokens := superAdminTokens(t, db)

	// Force the freshly minted access token into the past, so the very
	// first request the session makes has to refresh.
	if _, err := db.ExecContext(t.Context(),
		`UPDATE api_tokens SET expires_at = datetime('now', '-1 hour') WHERE id = ?`, tokens.AccessTokenId); err != nil {
		t.Fatal(err)
	}

	var refreshed wpcalc.TokenPair
	refreshCount := 0
	sess, err := wpcalc.New(ts.URL+"/api/v1", tokens, wpcalc.WithOnRefresh(func(p wpcalc.TokenPair) {
		refreshCount++
		refreshed = p
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	resp, err := sess.ListTenantsWithResponse(t.Context())
	if err != nil {
		t.Fatalf("ListTenantsWithResponse: %v", err)
	}
	if resp.StatusCode() != 200 {
		t.Fatalf("status %d after auto-refresh: %s", resp.StatusCode(), resp.Body)
	}
	if refreshCount != 1 {
		t.Fatalf("onRefresh called %d times, want 1", refreshCount)
	}
	if refreshed.AccessToken == tokens.AccessToken {
		t.Fatal("refreshed access token is identical to the expired one")
	}
	if sess.Tokens().AccessToken != refreshed.AccessToken {
		t.Fatal("Session.Tokens() did not update to the refreshed pair")
	}

	// The refresh token that was just spent must not work again — proves
	// the auto-refresh path used the real, single-use exchange rather
	// than, say, silently minting a fresh pair some other way.
	_, err = db.ExchangeRefreshToken(t.Context(), tokens.RefreshToken)
	if !errors.Is(err, store.ErrRefreshTokenUsed) {
		t.Fatalf("original refresh token: err = %v, want ErrRefreshTokenUsed", err)
	}
}

func TestSessionConcurrentRequestsCoalesceIntoOneRefresh(t *testing.T) {
	ts, db := testServer(t)
	tokens := superAdminTokens(t, db)
	if _, err := db.ExecContext(t.Context(),
		`UPDATE api_tokens SET expires_at = datetime('now', '-1 hour') WHERE id = ?`, tokens.AccessTokenId); err != nil {
		t.Fatal(err)
	}

	// No extra locking needed here: Session.refresh always calls onRefresh
	// while holding its own mutex, so concurrent invocations are already
	// serialized — and every goroutine's completion is observed through
	// the errs channel below before this count is read.
	var refreshCount int
	sess, err := wpcalc.New(ts.URL+"/api/v1", tokens, wpcalc.WithOnRefresh(func(wpcalc.TokenPair) {
		refreshCount++
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const n = 8
	errs := make(chan error, n)
	for range n {
		go func() {
			resp, err := sess.ListTenantsWithResponse(context.Background())
			if err != nil {
				errs <- err
				return
			}
			if resp.StatusCode() != 200 {
				errs <- errors.New("unexpected status")
				return
			}
			errs <- nil
		}()
	}
	for range n {
		if err := <-errs; err != nil {
			t.Errorf("concurrent request failed: %v", err)
		}
	}
	if refreshCount != 1 {
		t.Fatalf("onRefresh called %d times, want exactly 1", refreshCount)
	}
}
