package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"source.simonet.internal/rolsim/wpcalc/internal/domain"
	"source.simonet.internal/rolsim/wpcalc/internal/store"
)

func cmdUser(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	role := fs.String("role", domain.RoleUser, "role for a new account: admin or user")
	lang := fs.String("lang", "",
		"interface language for the account: de-CH, en, or empty to follow the browser")
	weak := fs.Bool("allow-weak-password", false,
		"accept a password below the minimum length (local development only)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("user: want one of add, passwd, lang, list")
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
	case "add":
		return userAdd(ctx, db, arg(1), *role, *lang, *weak)
	case "lang":
		return userLang(ctx, db, arg(1), arg(2))
	case "passwd":
		return userPasswd(ctx, db, arg(1), *weak)
	case "list":
		return userList(ctx, db)
	default:
		return fmt.Errorf("user: unknown action %q (want add, passwd, lang, or list)", action)
	}
}

func userAdd(ctx context.Context, db *store.DB, username, role, lang string, weak bool) error {
	if username == "" {
		return errors.New("user add: username is required")
	}
	if err := domain.ValidRole(role); err != nil {
		return err
	}

	pw, err := readPassword(true, weak)
	if err != nil {
		return err
	}
	id, err := db.CreateUserWeak(ctx, username, pw, role, weak)
	if err != nil {
		if errors.Is(err, store.ErrDuplicateUsername) {
			return fmt.Errorf("user add: %q already exists", username)
		}
		return err
	}

	shown := "automatic"
	if lang = normaliseLang(lang); lang != "" {
		if err := db.SetUserLanguage(ctx, id, lang); err != nil {
			return err
		}
		shown = lang
	}
	fmt.Printf("created %s (%s, language: %s)\n", username, role, shown)
	return nil
}

// userLang sets or clears an account's interface language.
func userLang(ctx context.Context, db *store.DB, username, lang string) error {
	if username == "" {
		return errors.New("user lang: username is required")
	}
	u, err := db.UserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("user lang: no such user %q", username)
		}
		return err
	}
	lang = normaliseLang(lang)
	if err := db.SetUserLanguage(ctx, u.ID, lang); err != nil {
		return err
	}
	if lang == "" {
		fmt.Printf("%s now follows the browser language\n", u.Username)
		return nil
	}
	fmt.Printf("%s now uses %s\n", u.Username, lang)
	return nil
}

// normaliseLang accepts the POSIX spelling as well as BCP 47, because "de_CH"
// is what a shell locale looks like and what people type.
func normaliseLang(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "_", "-")
}

func userPasswd(ctx context.Context, db *store.DB, username string, weak bool) error {
	if username == "" {
		return errors.New("user passwd: username is required")
	}
	pw, err := readPassword(true, weak)
	if err != nil {
		return err
	}
	if err := db.SetPasswordWeak(ctx, username, pw, weak); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("user passwd: no such user %q", username)
		}
		return err
	}
	fmt.Printf("password changed for %s; existing sessions revoked\n", username)
	return nil
}

func userList(ctx context.Context, db *store.DB) error {
	users, err := db.Users(ctx)
	if err != nil {
		return err
	}
	if len(users) == 0 {
		fmt.Println("no accounts yet — create one with `wpcalc user add <name> -role admin`")
		return nil
	}
	for _, u := range users {
		lang := u.Language
		if lang == "" {
			lang = "auto"
		}
		fmt.Printf("%-24s %-8s %s\n", u.Username, u.Role, lang)
	}
	return nil
}

// readPassword reads a password without echoing it.
//
// When stdin is not a terminal it falls back to reading a line, so the
// container-based e2e tests and any provisioning script can pipe one in. That
// fallback is announced on stderr rather than being silent, because a password
// typed into a non-terminal would otherwise be echoed with no warning.
func readPassword(confirm, allowWeak bool) (string, error) {
	check := domain.ValidPassword
	if allowWeak {
		// Loud on stderr: a short password is a deliberate local-development
		// choice, and it should never pass unremarked into a real database.
		fmt.Fprintln(os.Stderr, "warning: --allow-weak-password is set; the minimum length is not enforced")
		check = func(string) error { return nil }
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintln(os.Stderr, "warning: stdin is not a terminal; reading the password from the pipe without echo protection")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read password: %w", err)
		}
		pw := strings.TrimRight(line, "\r\n")
		if pw == "" {
			return "", errors.New("password is empty")
		}
		return pw, check(pw)
	}

	fmt.Fprint(os.Stderr, "Password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if err := check(string(first)); err != nil {
		return "", err
	}

	if confirm {
		fmt.Fprint(os.Stderr, "Repeat: ")
		second, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		if string(first) != string(second) {
			return "", errors.New("passwords do not match")
		}
	}
	return string(first), nil
}
