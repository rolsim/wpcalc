package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"time"

	"github.com/rolsim/wpcalc/internal/store"
)

func cmdToken(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	name := fs.String("name", "", "a label for the token pair, shown back by `token list` (default: the username)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("token: want one of create, refresh, list, revoke, revoke-all")
	}
	arg := func(i int) string {
		if i < len(positional) {
			return positional[i]
		}
		return ""
	}

	db, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	switch action := arg(0); action {
	case "create":
		return tokenCreate(ctx, db, arg(1), *name)
	case "refresh":
		return tokenRefresh(ctx, db, arg(1))
	case "list":
		return tokenList(ctx, db, arg(1))
	case "revoke":
		return tokenRevoke(ctx, db, arg(1))
	case "revoke-all":
		return tokenRevokeAll(ctx, db, arg(1))
	default:
		return fmt.Errorf("token: unknown action %q (want create, refresh, list, revoke, or revoke-all)", action)
	}
}

// tokenCreate mints an access/refresh pair for /api/v1. This is the only
// time either plaintext is shown — only their hashes are ever stored — so
// it prints both to stdout once and they are otherwise gone if not
// captured here.
func tokenCreate(ctx context.Context, db *store.DB, username, name string) error {
	if username == "" {
		return errors.New("token create: username is required")
	}
	if name == "" {
		name = username
	}
	u, err := db.UserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("token create: no such user %q", username)
		}
		return err
	}
	accessToken, accessID, accessExpiry, err := db.CreateAPIToken(ctx, u.ID, name)
	if err != nil {
		return fmt.Errorf("token create: %w", err)
	}
	refreshToken, _, refreshExpiry, err := db.CreateRefreshToken(ctx, u.ID, name)
	if err != nil {
		return fmt.Errorf("token create: %w", err)
	}
	printTokenPair(username, accessID, accessToken, accessExpiry, refreshToken, refreshExpiry)
	return nil
}

// tokenRefresh exchanges a refresh token for a new pair from the CLI —
// mostly for testing the flow without an HTTP round trip; an operator
// with database access can just run `token create` again just as easily.
func tokenRefresh(ctx context.Context, db *store.DB, refreshToken string) error {
	if refreshToken == "" {
		return errors.New("token refresh: refresh token is required")
	}
	ex, err := db.ExchangeRefreshToken(ctx, refreshToken)
	if err != nil {
		if errors.Is(err, store.ErrRefreshTokenUsed) {
			return errors.New("token refresh: that refresh token was already used — its whole pair should be treated as compromised")
		}
		if errors.Is(err, store.ErrNotFound) {
			return errors.New("token refresh: unknown, expired, or revoked refresh token")
		}
		return fmt.Errorf("token refresh: %w", err)
	}
	printTokenPair(ex.Name, ex.AccessTokenID, ex.AccessToken, ex.AccessTokenExpiresAt, ex.RefreshToken, ex.RefreshTokenExpiresAt)
	return nil
}

func printTokenPair(label string, accessID int64, accessToken string, accessExpiry time.Time, refreshToken string, refreshExpiry time.Time) {
	fmt.Printf(`token %d created for %s

access token (expires %s):
  %s

refresh token (expires %s, single-use — exchange it with 'wpcalc token refresh' before then):
  %s

Both are shown once — store them now. Use the access token as:
  Authorization: Bearer %s
`,
		accessID, label,
		accessExpiry.Format(time.RFC3339), accessToken,
		refreshExpiry.Format(time.RFC3339), refreshToken,
		accessToken)
}

func tokenList(ctx context.Context, db *store.DB, username string) error {
	if username == "" {
		return errors.New("token list: username is required")
	}
	u, err := db.UserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("token list: no such user %q", username)
		}
		return err
	}
	tokens, err := db.APITokens(ctx, u.ID)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		fmt.Printf("%s holds no api tokens — create one with `wpcalc token create %s`\n", username, username)
		return nil
	}
	now := time.Now()
	for _, t := range tokens {
		status := "active"
		switch {
		case t.RevokedAt != nil:
			status = "revoked " + t.RevokedAt.Format("2006-01-02")
		case now.After(t.ExpiresAt):
			status = "expired " + t.ExpiresAt.Format("2006-01-02 15:04")
		}
		lastUsed := "never used"
		if t.LastUsedAt != nil {
			lastUsed = "last used " + t.LastUsedAt.Format("2006-01-02")
		}
		fmt.Printf("%-4d %-20s created %s, %s, %s\n",
			t.ID, t.Name, t.CreatedAt.Format("2006-01-02"), lastUsed, status)
	}
	return nil
}

func tokenRevoke(ctx context.Context, db *store.DB, idArg string) error {
	id, err := strconv.ParseInt(idArg, 10, 64)
	if err != nil {
		return fmt.Errorf("token revoke: %q is not a valid token id", idArg)
	}
	if err := db.RevokeAPIToken(ctx, id); err != nil {
		return fmt.Errorf("token revoke: %w", err)
	}
	fmt.Printf("token %d revoked\n", id)
	return nil
}

// tokenRevokeAll revokes every access token and refresh token an account
// holds — "log out everywhere" for a compromised account, without needing
// to look up and revoke each one by id individually.
func tokenRevokeAll(ctx context.Context, db *store.DB, username string) error {
	if username == "" {
		return errors.New("token revoke-all: username is required")
	}
	u, err := db.UserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("token revoke-all: no such user %q", username)
		}
		return err
	}
	if err := db.RevokeAllUserTokens(ctx, u.ID); err != nil {
		return fmt.Errorf("token revoke-all: %w", err)
	}
	fmt.Printf("every token for %s revoked\n", username)
	return nil
}
