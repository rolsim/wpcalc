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

	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/store"
)

// cmdUser is deliberately narrow: add, grant, and revoke are the two
// bootstrap primitives no API client can perform on its own (creating the
// first account, and granting it a role — without which even a valid
// token can pass no permission check at all) plus their natural
// counterpart. Everything else an account holder can do to their own or
// another account — passwd, lang, roles, list — has a /api/v1 equivalent
// and lives in wpcalcctl instead, which needs no direct database access
// at all. See docs/en/admin.md.
func cmdUser(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	dbPath := fs.String("db", defaultDBPath(), "path to the SQLite database")
	lang := fs.String("lang", "",
		"interface language for the account: de-CH, en, or empty to follow the browser (user add)")
	weak := fs.Bool("allow-weak-password", false,
		"accept a password below the minimum length (local development only)")
	system := fs.Bool("system", false, "grant/revoke a system-scope role (user grant|revoke)")
	tenant := fs.Int64("tenant", 0, "grant/revoke a tenant-scope role for this tenant id (user grant|revoke)")
	employee := fs.Int64("employee", 0, "grant/revoke an employee-scope role for this employee id (user grant|revoke)")
	role := fs.String("role", "", "role id to grant (user grant)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("user: want one of add, grant, revoke")
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
		return userAdd(ctx, db, arg(1), *lang, *weak)
	case "grant":
		return userGrant(ctx, db, arg(1), *system, *tenant, *employee, *role)
	case "revoke":
		return userRevoke(ctx, db, arg(1), *system, *tenant, *employee)
	default:
		return fmt.Errorf("user: unknown action %q (want add, grant, or revoke)", action)
	}
}

// userAdd creates a bare account with no access at all — role assignment is
// a separate step (see userGrant), so there is never a moment where an
// account holds an implicit role nobody asked for.
func userAdd(ctx context.Context, db *store.DB, username, lang string, weak bool) error {
	if username == "" {
		return errors.New("user add: username is required")
	}

	pw, err := readPassword(true, weak)
	if err != nil {
		return err
	}
	id, err := db.CreateUserWeak(ctx, username, pw, weak)
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
	fmt.Printf("created %s (language: %s) — no access yet; grant a role with `wpcalc user grant`\n", username, shown)
	return nil
}

// normaliseLang accepts the POSIX spelling as well as BCP 47, because "de_CH"
// is what a shell locale looks like and what people type.
func normaliseLang(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "_", "-")
}

// userGrant assigns a role at exactly one scope: --system (no target),
// --tenant ID, or --employee ID. The scope must match the role's own scope
// (roles.scope) — enforced by the store, which the database's own trigger
// enforces again underneath.
func userGrant(ctx context.Context, db *store.DB, username string, system bool, tenant, employee int64, roleID string) error {
	if username == "" {
		return errors.New("user grant: username is required")
	}
	if roleID == "" {
		return errors.New("user grant: -role is required")
	}
	tenantID, employeeID, err := scopeTarget(system, tenant, employee)
	if err != nil {
		return fmt.Errorf("user grant: %w", err)
	}

	u, err := db.UserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("user grant: no such user %q", username)
		}
		return err
	}
	if err := db.GrantUserRole(ctx, u.ID, tenantID, employeeID, roleID); err != nil {
		return fmt.Errorf("user grant: %w", err)
	}
	fmt.Printf("%s granted %s%s\n", username, roleID, scopeDescription(tenantID, employeeID))
	return nil
}

// userRevoke removes whatever role a user holds at exactly one scope.
func userRevoke(ctx context.Context, db *store.DB, username string, system bool, tenant, employee int64) error {
	if username == "" {
		return errors.New("user revoke: username is required")
	}
	tenantID, employeeID, err := scopeTarget(system, tenant, employee)
	if err != nil {
		return fmt.Errorf("user revoke: %w", err)
	}

	u, err := db.UserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("user revoke: no such user %q", username)
		}
		return err
	}
	if err := db.RevokeUserRole(ctx, u.ID, tenantID, employeeID); err != nil {
		return fmt.Errorf("user revoke: %w", err)
	}
	fmt.Printf("%s's role%s revoked\n", username, scopeDescription(tenantID, employeeID))
	return nil
}

// scopeTarget turns the three mutually exclusive scope flags into the
// tenant_id/employee_id pair user_roles expects (nil/nil meaning system).
func scopeTarget(system bool, tenant, employee int64) (tenantID, employeeID *int64, err error) {
	set := 0
	if system {
		set++
	}
	if tenant != 0 {
		set++
	}
	if employee != 0 {
		set++
	}
	if set != 1 {
		return nil, nil, errors.New("exactly one of -system, -tenant, or -employee is required")
	}
	if tenant != 0 {
		return &tenant, nil, nil
	}
	if employee != 0 {
		return nil, &employee, nil
	}
	return nil, nil, nil
}

func scopeDescription(tenantID, employeeID *int64) string {
	switch {
	case tenantID != nil:
		return fmt.Sprintf(" (tenant %d)", *tenantID)
	case employeeID != nil:
		return fmt.Sprintf(" (employee %d)", *employeeID)
	default:
		return " (system-wide)"
	}
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
