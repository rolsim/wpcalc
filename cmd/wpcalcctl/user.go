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

	wpcalc "github.com/rolsim/wpcalc/sdk/go"
)

func cmdUser(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("user", flag.ContinueOnError)
	lang := fs.String("lang", "", "interface language: de-CH, en, or empty to follow the browser (user lang)")
	system := fs.Bool("system", false, "grant/revoke a system-scope role (user grant|revoke)")
	tenant := fs.Int64("tenant", 0, "grant/revoke a tenant-scope role for this tenant id; also required alongside -employee (user grant|revoke)")
	employee := fs.Int64("employee", 0, "grant/revoke an employee-scope role for this employee id — needs -tenant too (user grant|revoke)")
	role := fs.String("role", "", "role id to grant (user grant)")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		return errors.New("user: want one of add, passwd, lang, roles, list, grant, revoke")
	}
	arg := func(i int) string {
		if i < len(positional) {
			return positional[i]
		}
		return ""
	}

	sess, err := newSession()
	if err != nil {
		return err
	}

	switch action := arg(0); action {
	case "add":
		return userAdd(ctx, sess, arg(1))
	case "passwd":
		return userPasswd(ctx, sess, arg(1))
	case "lang":
		return userLang(ctx, sess, arg(1), *lang)
	case "roles":
		return userRoles(ctx, sess, arg(1))
	case "list":
		return userList(ctx, sess)
	case "grant":
		return userGrant(ctx, sess, arg(1), *system, *tenant, *employee, *role)
	case "revoke":
		return userRevoke(ctx, sess, arg(1), *system, *tenant, *employee)
	default:
		return fmt.Errorf("user: unknown action %q (want add, passwd, lang, roles, list, grant, or revoke)", action)
	}
}

// userAdd creates an account with no access at all — matches the server's
// own `wpcalc user add` bootstrap command, just reachable remotely for
// accounts after the first one, via manage_users.
func userAdd(ctx context.Context, sess *wpcalc.Session, username string) error {
	if username == "" {
		return errors.New("user add: username is required")
	}
	pw, err := readPassword(true)
	if err != nil {
		return err
	}
	resp, err := sess.CreateUserWithResponse(ctx, wpcalc.CreateUserJSONRequestBody{Username: username, Password: pw})
	if err != nil {
		return fmt.Errorf("user add: %w", err)
	}
	if resp.JSON201 == nil {
		return apiError("user add", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	fmt.Printf("created %s — no access yet; grant a role with `wpcalcctl user grant`\n", resp.JSON201.Username)
	return nil
}

func userPasswd(ctx context.Context, sess *wpcalc.Session, username string) error {
	if username == "" {
		return errors.New("user passwd: username is required")
	}
	pw, err := readPassword(true)
	if err != nil {
		return err
	}
	resp, err := sess.SetUserPasswordWithResponse(ctx, username, wpcalc.SetUserPasswordJSONRequestBody{Password: pw})
	if err != nil {
		return fmt.Errorf("user passwd: %w", err)
	}
	if resp.StatusCode() != 204 {
		return apiError("user passwd", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	fmt.Printf("password changed for %s; existing sessions revoked\n", username)
	return nil
}

func userLang(ctx context.Context, sess *wpcalc.Session, username, lang string) error {
	if username == "" {
		return errors.New("user lang: username is required")
	}
	lang = normaliseLang(lang)
	body := wpcalc.SetUserLanguageJSONRequestBody{}
	if lang != "" {
		body.Lang = &lang
	}
	resp, err := sess.SetUserLanguageWithResponse(ctx, username, body)
	if err != nil {
		return fmt.Errorf("user lang: %w", err)
	}
	if resp.StatusCode() != 204 {
		return apiError("user lang", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	if lang == "" {
		fmt.Printf("%s now follows the browser language\n", username)
		return nil
	}
	fmt.Printf("%s now uses %s\n", username, lang)
	return nil
}

func normaliseLang(s string) string {
	return strings.ReplaceAll(strings.TrimSpace(s), "_", "-")
}

func userRoles(ctx context.Context, sess *wpcalc.Session, username string) error {
	if username == "" {
		return errors.New("user roles: username is required")
	}
	resp, err := sess.GetUserRolesWithResponse(ctx, username)
	if err != nil {
		return fmt.Errorf("user roles: %w", err)
	}
	if resp.JSON200 == nil {
		return apiError("user roles", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	if len(*resp.JSON200) == 0 {
		fmt.Printf("%s holds no roles yet\n", username)
		return nil
	}
	for _, r := range *resp.JSON200 {
		fmt.Printf("%-20s%s\n", r.RoleId, roleAssignmentScopeDescription(r.TenantId, r.EmployeeId))
	}
	return nil
}

func userList(ctx context.Context, sess *wpcalc.Session) error {
	resp, err := sess.ListUsersWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("user list: %w", err)
	}
	if resp.JSON200 == nil {
		return apiError("user list", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	if len(*resp.JSON200) == 0 {
		fmt.Println("no accounts yet — create one with `wpcalcctl user add <name>`")
		return nil
	}
	for _, u := range *resp.JSON200 {
		lang := u.Language
		if lang == "" {
			lang = "auto"
		}
		fmt.Printf("%-24s %s\n", u.Username, lang)
	}
	return nil
}

// userGrant assigns a role at exactly one scope: -system (no target),
// -tenant ID (tenant scope), or -tenant ID -employee ID (employee scope —
// the API nests employee-role-assignments under a tenant, so both are
// needed together).
func userGrant(ctx context.Context, sess *wpcalc.Session, username string, system bool, tenant, employee int64, roleID string) error {
	if username == "" {
		return errors.New("user grant: username is required")
	}
	if roleID == "" {
		return errors.New("user grant: -role is required")
	}
	switch {
	case system && tenant == 0 && employee == 0:
		resp, err := sess.GrantRoleWithResponse(ctx, wpcalc.GrantRoleJSONRequestBody{Username: username, RoleId: roleID})
		if err != nil {
			return fmt.Errorf("user grant: %w", err)
		}
		if resp.StatusCode() != 204 {
			return apiError("user grant", resp.StatusCode(), resp.Body, resp.JSONDefault)
		}
	case !system && tenant != 0 && employee == 0:
		resp, err := sess.GrantRoleWithResponse(ctx, wpcalc.GrantRoleJSONRequestBody{Username: username, RoleId: roleID, TenantId: &tenant})
		if err != nil {
			return fmt.Errorf("user grant: %w", err)
		}
		if resp.StatusCode() != 204 {
			return apiError("user grant", resp.StatusCode(), resp.Body, resp.JSONDefault)
		}
	case !system && tenant != 0 && employee != 0:
		resp, err := sess.GrantEmployeeRoleWithResponse(ctx, tenant, wpcalc.GrantEmployeeRoleJSONRequestBody{
			Username: username, EmployeeId: employee, RoleId: roleID,
		})
		if err != nil {
			return fmt.Errorf("user grant: %w", err)
		}
		if resp.StatusCode() != 204 {
			return apiError("user grant", resp.StatusCode(), resp.Body, resp.JSONDefault)
		}
	default:
		return errors.New("user grant: use -system, -tenant ID, or -tenant ID -employee ID")
	}
	fmt.Printf("%s granted %s%s\n", username, roleID, grantScopeDescription(system, tenant, employee))
	return nil
}

func userRevoke(ctx context.Context, sess *wpcalc.Session, username string, system bool, tenant, employee int64) error {
	if username == "" {
		return errors.New("user revoke: username is required")
	}
	userID, err := resolveUserID(ctx, sess, username)
	if err != nil {
		return fmt.Errorf("user revoke: %w", err)
	}

	switch {
	case system && tenant == 0 && employee == 0:
		resp, err := sess.RevokeRoleWithResponse(ctx, wpcalc.RevokeRoleJSONRequestBody{UserId: userID})
		if err != nil {
			return fmt.Errorf("user revoke: %w", err)
		}
		if resp.StatusCode() != 204 {
			return apiError("user revoke", resp.StatusCode(), resp.Body, resp.JSONDefault)
		}
	case !system && tenant != 0 && employee == 0:
		resp, err := sess.RevokeRoleWithResponse(ctx, wpcalc.RevokeRoleJSONRequestBody{UserId: userID, TenantId: &tenant})
		if err != nil {
			return fmt.Errorf("user revoke: %w", err)
		}
		if resp.StatusCode() != 204 {
			return apiError("user revoke", resp.StatusCode(), resp.Body, resp.JSONDefault)
		}
	case !system && tenant != 0 && employee != 0:
		resp, err := sess.RevokeEmployeeRoleWithResponse(ctx, tenant, wpcalc.RevokeEmployeeRoleJSONRequestBody{
			UserId: userID, EmployeeId: employee,
		})
		if err != nil {
			return fmt.Errorf("user revoke: %w", err)
		}
		if resp.StatusCode() != 204 {
			return apiError("user revoke", resp.StatusCode(), resp.Body, resp.JSONDefault)
		}
	default:
		return errors.New("user revoke: use -system, -tenant ID, or -tenant ID -employee ID")
	}
	fmt.Printf("%s's role%s revoked\n", username, grantScopeDescription(system, tenant, employee))
	return nil
}

// resolveUserID looks up an account's id by username — needed for revoke,
// where the API takes a userId (grant takes a username directly, no
// lookup needed there). Requires manage_users system-wide, the same
// permission RevokeRole/RevokeEmployeeRole themselves require, so this
// adds no new access requirement for anyone who could revoke anyway.
func resolveUserID(ctx context.Context, sess *wpcalc.Session, username string) (int64, error) {
	resp, err := sess.ListUsersWithResponse(ctx)
	if err != nil {
		return 0, err
	}
	if resp.JSON200 == nil {
		return 0, apiError("look up user", resp.StatusCode(), resp.Body, resp.JSONDefault)
	}
	for _, u := range *resp.JSON200 {
		if u.Username == username {
			return u.Id, nil
		}
	}
	return 0, fmt.Errorf("no such user %q", username)
}

func grantScopeDescription(system bool, tenant, employee int64) string {
	switch {
	case employee != 0:
		return fmt.Sprintf(" (employee %d, tenant %d)", employee, tenant)
	case tenant != 0:
		return fmt.Sprintf(" (tenant %d)", tenant)
	case system:
		return " (system-wide)"
	default:
		return ""
	}
}

func roleAssignmentScopeDescription(tenantID, employeeID *int64) string {
	switch {
	case employeeID != nil:
		return fmt.Sprintf(" (employee %d)", *employeeID)
	case tenantID != nil:
		return fmt.Sprintf(" (tenant %d)", *tenantID)
	default:
		return " (system-wide)"
	}
}

// readPassword reads a password without echoing it, falling back to a
// plain line read when stdin isn't a terminal (scripts, tests).
func readPassword(confirm bool) (string, error) {
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
		return pw, nil
	}

	fmt.Fprint(os.Stderr, "Password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
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
