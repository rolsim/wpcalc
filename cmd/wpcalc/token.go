package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"

	"github.com/rolsim/wpcalc/internal/store"
)

func cmdToken(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	name := fs.String("name", "", "a label for the token, shown back by `token list` (default: the username)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("token: want one of create, list, revoke")
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
	case "list":
		return tokenList(ctx, db, arg(1))
	case "revoke":
		return tokenRevoke(ctx, db, arg(1))
	default:
		return fmt.Errorf("token: unknown action %q (want create, list, or revoke)", action)
	}
}

// tokenCreate mints a bearer credential for /api/v1. This is the only time
// the plaintext is shown — only its hash is ever stored — so it prints to
// stdout once and is otherwise gone if not captured here.
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
	token, id, err := db.CreateAPIToken(ctx, u.ID, name)
	if err != nil {
		return fmt.Errorf("token create: %w", err)
	}
	fmt.Printf("token %d created for %s\n\n%s\n\nThis is shown once — store it now. Use it as:\n  Authorization: Bearer %s\n",
		id, username, token, token)
	return nil
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
	for _, t := range tokens {
		status := "active"
		if t.RevokedAt != nil {
			status = "revoked " + t.RevokedAt.Format("2006-01-02")
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
