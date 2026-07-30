package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/rolsim/wpcalc/internal/store"
)

// cmdToken is deliberately narrow: create is the bootstrap primitive no
// API client can perform on its own — an endpoint that requires a bearer
// token cannot be how an account gets its first one. Listing, revoking, and
// minting *additional* tokens for an account that already has one are all
// self-service via /api/v1 (see wpcalcctl's own `token` command), which
// needs no direct database access at all.
func cmdToken(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	name := fs.String("name", "", "a label for the token pair, shown back by `wpcalcctl token list` (default: the username)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("token: want create")
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
	default:
		return fmt.Errorf("token: unknown action %q (want create)", action)
	}
}

// tokenCreate mints an access/refresh pair for /api/v1. This is the only
// time either plaintext is shown — only their hashes are ever stored — so
// it prints both to stdout once and they are otherwise gone if not
// captured here. Use them with `wpcalcctl login` to store credentials for
// every other administrative action.
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

func printTokenPair(label string, accessID int64, accessToken string, accessExpiry time.Time, refreshToken string, refreshExpiry time.Time) {
	fmt.Printf(`token %d created for %s

access token (expires %s):
  %s

refresh token (expires %s, single-use — wpcalcctl exchanges it automatically as needed):
  %s

Both are shown once — store them now, or hand them straight to:
  wpcalcctl login --server URL --access-token %s --refresh-token %s
`,
		accessID, label,
		accessExpiry.Format(time.RFC3339), accessToken,
		refreshExpiry.Format(time.RFC3339), refreshToken,
		accessToken, refreshToken)
}
