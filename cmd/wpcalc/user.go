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
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("user: want one of add, passwd, list")
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
		return userAdd(ctx, db, arg(1), *role)
	case "passwd":
		return userPasswd(ctx, db, arg(1))
	case "list":
		return userList(ctx, db)
	default:
		return fmt.Errorf("user: unknown action %q (want add, passwd, or list)", action)
	}
}

func userAdd(ctx context.Context, db *store.DB, username, role string) error {
	if username == "" {
		return errors.New("user add: username is required")
	}
	if err := domain.ValidRole(role); err != nil {
		return err
	}

	pw, err := readPassword(true)
	if err != nil {
		return err
	}
	if _, err := db.CreateUser(ctx, username, pw, role); err != nil {
		if errors.Is(err, store.ErrDuplicateUsername) {
			return fmt.Errorf("user add: %q already exists", username)
		}
		return err
	}
	fmt.Printf("created %s (%s)\n", username, role)
	return nil
}

func userPasswd(ctx context.Context, db *store.DB, username string) error {
	if username == "" {
		return errors.New("user passwd: username is required")
	}
	pw, err := readPassword(true)
	if err != nil {
		return err
	}
	if err := db.SetPassword(ctx, username, pw); err != nil {
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
		fmt.Printf("%-24s %s\n", u.Username, u.Role)
	}
	return nil
}

// readPassword reads a password without echoing it.
//
// When stdin is not a terminal it falls back to reading a line, so the
// container-based e2e tests and any provisioning script can pipe one in. That
// fallback is announced on stderr rather than being silent, because a password
// typed into a non-terminal would otherwise be echoed with no warning.
func readPassword(confirm bool) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintln(os.Stderr, "warning: stdin is not a terminal; reading the password from the pipe without echo protection")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read password: %w", err)
		}
		pw := strings.TrimRight(line, "\r\n")
		return pw, domain.ValidPassword(pw)
	}

	fmt.Fprint(os.Stderr, "Password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if err := domain.ValidPassword(string(first)); err != nil {
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
