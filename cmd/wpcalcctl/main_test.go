package main

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/httpx"
	"github.com/rolsim/wpcalc/internal/i18n"
	"github.com/rolsim/wpcalc/internal/store"
)

// testServer spins up a real wpcalc server, the same way sdk/go's own
// tests do — real SQLite file, real migrations, real handler tree.
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

// bootstrapAdmin creates a super-admin account and its first token pair
// directly against the store — exactly what `wpcalc user add`/`grant` and
// `wpcalc token create` do on the real server binary — then points this
// test at a scratch credentials file and logs the CLI in.
func bootstrapAdmin(t *testing.T, ts *httptest.Server, db *store.DB) {
	t.Helper()
	ctx := t.Context()
	uid, err := db.CreateUserWeak(ctx, "cli-admin", "x", true)
	if err != nil {
		t.Fatalf("CreateUserWeak: %v", err)
	}
	if err := db.GrantUserRole(ctx, uid, nil, nil, "super_admin"); err != nil {
		t.Fatalf("GrantUserRole: %v", err)
	}
	access, _, _, err := db.CreateAPIToken(ctx, uid, "cli-test")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	refresh, _, _, err := db.CreateRefreshToken(ctx, uid, "cli-test")
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}

	t.Setenv("WPCALCCTL_CREDENTIALS", filepath.Join(t.TempDir(), "credentials.json"))
	if err := run([]string{
		"login",
		"--server", ts.URL + "/api/v1",
		"--access-token", access,
		"--refresh-token", refresh,
	}); err != nil {
		t.Fatalf("login: %v", err)
	}
}

// withStdin temporarily replaces os.Stdin with a pipe containing content —
// readPassword's non-terminal fallback path reads exactly this.
func withStdin(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
	go func() {
		_, _ = w.WriteString(content)
		_ = w.Close()
	}()
}

func TestLoginStoresWorkingCredentials(t *testing.T) {
	ts, db := testServer(t)
	bootstrapAdmin(t, ts, db)

	creds, err := loadCredentials()
	if err != nil {
		t.Fatalf("loadCredentials: %v", err)
	}
	if creds.Server != ts.URL+"/api/v1" {
		t.Fatalf("Server = %q", creds.Server)
	}
	if creds.Tokens.AccessToken == "" || creds.Tokens.RefreshToken == "" {
		t.Fatalf("Tokens = %+v", creds.Tokens)
	}
}

func TestTenantAddListRename(t *testing.T) {
	ts, db := testServer(t)
	bootstrapAdmin(t, ts, db)

	if err := run([]string{"tenant", "add", "Acme"}); err != nil {
		t.Fatalf("tenant add: %v", err)
	}
	tenants, err := db.Tenants(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var acmeID int64
	found := false
	for _, tn := range tenants {
		if tn.Name == "Acme" {
			found, acmeID = true, tn.ID
		}
	}
	if !found {
		t.Fatalf("tenant not created; tenants = %v", tenants)
	}

	if err := run([]string{"tenant", "list"}); err != nil {
		t.Fatalf("tenant list: %v", err)
	}

	if err := run([]string{"tenant", "rename", itoa(acmeID), "Acme Renamed"}); err != nil {
		t.Fatalf("tenant rename: %v", err)
	}
	renamed, err := db.Tenant(t.Context(), acmeID)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "Acme Renamed" {
		t.Fatalf("tenant name = %q", renamed.Name)
	}
}

func TestRoleAndPermissionCommands(t *testing.T) {
	ts, db := testServer(t)
	bootstrapAdmin(t, ts, db)

	if err := run([]string{"role", "add", "auditor", "-name", "Auditor", "-scope", "tenant"}); err != nil {
		t.Fatalf("role add: %v", err)
	}
	role, err := db.Role(t.Context(), "auditor")
	if err != nil {
		t.Fatalf("role was not created: %v", err)
	}
	if role.Name != "Auditor" {
		t.Fatalf("role name = %q", role.Name)
	}

	if err := run([]string{"role", "permissions", "auditor", "-add", "read"}); err != nil {
		t.Fatalf("role permissions -add: %v", err)
	}
	perms, err := db.RolePermissionsFor(t.Context(), []string{"auditor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(perms["auditor"]) != 1 || perms["auditor"][0] != "read" {
		t.Fatalf("auditor permissions = %v", perms["auditor"])
	}

	if err := run([]string{"role", "list"}); err != nil {
		t.Fatalf("role list: %v", err)
	}
	if err := run([]string{"permission", "list"}); err != nil {
		t.Fatalf("permission list: %v", err)
	}

	if err := run([]string{"role", "delete", "auditor"}); err != nil {
		t.Fatalf("role delete: %v", err)
	}
	if _, err := db.Role(t.Context(), "auditor"); err == nil {
		t.Fatal("role still exists after delete")
	}
}

func TestUserLifecycleAndGrant(t *testing.T) {
	ts, db := testServer(t)
	bootstrapAdmin(t, ts, db)

	withStdin(t, "correcthorsebattery\ncorrecthorsebattery\n")
	if err := run([]string{"user", "add", "bob"}); err != nil {
		t.Fatalf("user add: %v", err)
	}
	bob, err := db.UserByUsername(t.Context(), "bob")
	if err != nil {
		t.Fatalf("user was not created: %v", err)
	}

	if err := run([]string{"user", "grant", "bob", "-tenant", "1", "-role", "mandant_admin"}); err != nil {
		t.Fatalf("user grant: %v", err)
	}
	roles, err := db.UserRolesForUser(t.Context(), bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 1 || roles[0].RoleID != "mandant_admin" {
		t.Fatalf("bob's roles = %v", roles)
	}

	if err := run([]string{"user", "roles", "bob"}); err != nil {
		t.Fatalf("user roles: %v", err)
	}
	if err := run([]string{"user", "list"}); err != nil {
		t.Fatalf("user list: %v", err)
	}

	if err := run([]string{"user", "revoke", "bob", "-tenant", "1"}); err != nil {
		t.Fatalf("user revoke: %v", err)
	}
	roles, err = db.UserRolesForUser(t.Context(), bob.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 0 {
		t.Fatalf("bob still holds roles after revoke: %v", roles)
	}

	withStdin(t, "anothergoodpassword\n")
	if err := run([]string{"user", "passwd", "bob"}); err != nil {
		t.Fatalf("user passwd: %v", err)
	}
	if err := run([]string{"user", "lang", "bob", "-lang", "de-CH"}); err != nil {
		t.Fatalf("user lang: %v", err)
	}
	bob, err = db.UserByUsername(t.Context(), "bob")
	if err != nil {
		t.Fatal(err)
	}
	if bob.Language != "de-CH" {
		t.Fatalf("bob's language = %q", bob.Language)
	}
}

func TestTokenSelfServiceCommands(t *testing.T) {
	ts, db := testServer(t)
	bootstrapAdmin(t, ts, db)

	if err := run([]string{"token", "create", "-name", "second"}); err != nil {
		t.Fatalf("token create: %v", err)
	}
	if err := run([]string{"token", "list"}); err != nil {
		t.Fatalf("token list: %v", err)
	}

	admin, err := db.UserByUsername(t.Context(), "cli-admin")
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := db.APITokens(t.Context(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("tokens = %v, want 2 (bootstrap + second)", tokens)
	}
	var secondID int64
	for _, tk := range tokens {
		if tk.Name == "second" {
			secondID = tk.ID
		}
	}
	if secondID == 0 {
		t.Fatal("did not find the token named 'second'")
	}

	if err := run([]string{"token", "revoke", itoa(secondID)}); err != nil {
		t.Fatalf("token revoke: %v", err)
	}
	tokens, err = db.APITokens(t.Context(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tokens {
		if tk.ID == secondID && tk.RevokedAt == nil {
			t.Fatal("token was not revoked")
		}
	}

	if err := run([]string{"token", "revoke-all"}); err != nil {
		t.Fatalf("token revoke-all: %v", err)
	}
	tokens, err = db.APITokens(t.Context(), admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tokens {
		if tk.RevokedAt == nil {
			t.Fatalf("token %d not revoked after revoke-all", tk.ID)
		}
	}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
