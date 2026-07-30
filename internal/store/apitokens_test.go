package store

import (
	"errors"
	"testing"
	"time"

	"github.com/rolsim/wpcalc/internal/domain"
)

func TestAPITokenLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	uid, err := db.CreateUserWeak(ctx, "alice", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}

	token, id, expiresAt, err := db.CreateAPIToken(ctx, uid, "ci")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	if id == 0 {
		t.Fatal("zero id")
	}
	wantExpiry := time.Now().Add(domain.AccessTokenTTL)
	if diff := wantExpiry.Sub(expiresAt); diff < -time.Minute || diff > time.Minute {
		t.Fatalf("expiresAt = %v, want close to now+%v", expiresAt, domain.AccessTokenTTL)
	}

	u, err := db.UserByAPIToken(ctx, token)
	if err != nil {
		t.Fatalf("UserByAPIToken: %v", err)
	}
	if u.ID != uid {
		t.Fatalf("got user %d, want %d", u.ID, uid)
	}

	toks, err := db.APITokens(ctx, uid)
	if err != nil {
		t.Fatalf("APITokens: %v", err)
	}
	if len(toks) != 1 || toks[0].ID != id || toks[0].Name != "ci" {
		t.Fatalf("APITokens = %+v", toks)
	}
	if !toks[0].ExpiresAt.Equal(expiresAt) {
		t.Fatalf("listed ExpiresAt = %v, want %v", toks[0].ExpiresAt, expiresAt)
	}
	if toks[0].RevokedAt != nil {
		t.Fatalf("freshly created token already revoked: %+v", toks[0])
	}
	if toks[0].LastUsedAt == nil {
		t.Fatal("LastUsedAt should be set after UserByAPIToken touched it")
	}

	if err := db.RevokeAPIToken(ctx, id); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}
	if _, err := db.UserByAPIToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token: err = %v, want ErrNotFound", err)
	}
	if err := db.RevokeAPIToken(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("double revoke: err = %v, want ErrNotFound", err)
	}
}

func TestAPITokenExpires(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUserWeak(ctx, "carol", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	token, id, _, err := db.CreateAPIToken(ctx, uid, "expiring")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.UserByAPIToken(ctx, token); err != nil {
		t.Fatalf("fresh token should authenticate: %v", err)
	}

	if _, err := db.ExecContext(ctx, `UPDATE api_tokens SET expires_at = ? WHERE id = ?`,
		formatSQLiteTimestamp(time.Now().Add(-time.Minute)), id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.UserByAPIToken(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired token: err = %v, want ErrNotFound (indistinguishable from revoked/unknown)", err)
	}
}

func TestAPITokenUnknownSecretIsNotFound(t *testing.T) {
	db := testDB(t)
	if _, err := db.UserByAPIToken(t.Context(), "wpat_this-was-never-issued"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestAPITokenRequiresAName(t *testing.T) {
	db := testDB(t)
	uid, err := db.CreateUserWeak(t.Context(), "bob", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}
	if _, _, _, err := db.CreateAPIToken(t.Context(), uid, "  "); err == nil {
		t.Fatal("expected an error for a blank name")
	}
}

func TestAPITokensAreIsolatedPerUser(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	alice, err := db.CreateUserWeak(ctx, "alice2", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}
	bob, err := db.CreateUserWeak(ctx, "bob2", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}
	if _, _, _, err := db.CreateAPIToken(ctx, alice, "a"); err != nil {
		t.Fatal(err)
	}
	toks, err := db.APITokens(ctx, bob)
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 0 {
		t.Fatalf("bob sees alice's tokens: %+v", toks)
	}
}

func TestRefreshTokenExchangeIssuesAWorkingAccessToken(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUserWeak(ctx, "dave", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	refreshTok, _, _, err := db.CreateRefreshToken(ctx, uid, "ci")
	if err != nil {
		t.Fatal(err)
	}

	ex, err := db.ExchangeRefreshToken(ctx, refreshTok)
	if err != nil {
		t.Fatalf("ExchangeRefreshToken: %v", err)
	}
	if ex.Name != "ci" {
		t.Fatalf("Name = %q, want %q", ex.Name, "ci")
	}
	if _, err := db.UserByAPIToken(ctx, ex.AccessToken); err != nil {
		t.Fatalf("issued access token should authenticate: %v", err)
	}
}

func TestRefreshTokenIsSingleUse(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUserWeak(ctx, "erin", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	refreshTok, _, _, err := db.CreateRefreshToken(ctx, uid, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExchangeRefreshToken(ctx, refreshTok); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	if _, err := db.ExchangeRefreshToken(ctx, refreshTok); !errors.Is(err, ErrRefreshTokenUsed) {
		t.Fatalf("second exchange: err = %v, want ErrRefreshTokenUsed", err)
	}
}

func TestRefreshTokenRotates(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUserWeak(ctx, "frank", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	refreshTok, _, _, err := db.CreateRefreshToken(ctx, uid, "ci")
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.ExchangeRefreshToken(ctx, refreshTok)
	if err != nil {
		t.Fatal(err)
	}
	if first.RefreshToken == refreshTok {
		t.Fatal("exchange returned the same refresh token instead of a rotated one")
	}
	// The newly rotated refresh token works too — the chain continues.
	second, err := db.ExchangeRefreshToken(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("rotated refresh token should work: %v", err)
	}
	if _, err := db.UserByAPIToken(ctx, second.AccessToken); err != nil {
		t.Fatalf("access token from second exchange should authenticate: %v", err)
	}
}

func TestRefreshTokenUnknownSecretIsNotFound(t *testing.T) {
	db := testDB(t)
	if _, err := db.ExchangeRefreshToken(t.Context(), "wprt_never-issued"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRefreshTokenExpired(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUserWeak(ctx, "grace", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	refreshTok, id, _, err := db.CreateRefreshToken(ctx, uid, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE refresh_tokens SET expires_at = ? WHERE id = ?`,
		formatSQLiteTimestamp(time.Now().Add(-time.Minute)), id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExchangeRefreshToken(ctx, refreshTok); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestRevokeAllUserTokens(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUserWeak(ctx, "henry", "x", true)
	if err != nil {
		t.Fatal(err)
	}
	accessTok, _, _, err := db.CreateAPIToken(ctx, uid, "ci")
	if err != nil {
		t.Fatal(err)
	}
	refreshTok, _, _, err := db.CreateRefreshToken(ctx, uid, "ci")
	if err != nil {
		t.Fatal(err)
	}

	if err := db.RevokeAllUserTokens(ctx, uid); err != nil {
		t.Fatalf("RevokeAllUserTokens: %v", err)
	}
	if _, err := db.UserByAPIToken(ctx, accessTok); !errors.Is(err, ErrNotFound) {
		t.Fatalf("access token: err = %v, want ErrNotFound", err)
	}
	if _, err := db.ExchangeRefreshToken(ctx, refreshTok); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refresh token: err = %v, want ErrNotFound", err)
	}
}

func TestSetPasswordRevokesRefreshTokens(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	uid, err := db.CreateUserWeak(ctx, "iris", "oldpassword1", true)
	if err != nil {
		t.Fatal(err)
	}
	refreshTok, _, _, err := db.CreateRefreshToken(ctx, uid, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPasswordWeak(ctx, "iris", "newpassword1", true); err != nil {
		t.Fatalf("SetPasswordWeak: %v", err)
	}
	if _, err := db.ExchangeRefreshToken(ctx, refreshTok); !errors.Is(err, ErrNotFound) {
		t.Fatalf("refresh token survived a password change: err = %v, want ErrNotFound", err)
	}
}
