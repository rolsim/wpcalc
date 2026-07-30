package store

import (
	"errors"
	"testing"
)

func TestAPITokenLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	uid, err := db.CreateUserWeak(ctx, "alice", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}

	token, id, err := db.CreateAPIToken(ctx, uid, "ci")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	if id == 0 {
		t.Fatal("zero id")
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
	if _, _, err := db.CreateAPIToken(t.Context(), uid, "  "); err == nil {
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
	if _, _, err := db.CreateAPIToken(ctx, alice, "a"); err != nil {
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
