package store

import (
	"errors"
	"testing"
	"time"

	"source.simonet.internal/rolsim/wpcalc/internal/domain"
)

const goodPassword = "a-sufficiently-long-password"

func TestCreateAndAuthenticateUser(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	if _, err := db.CreateUser(ctx, "alice", goodPassword, domain.RoleAdmin); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	u, err := db.Authenticate(ctx, "alice", goodPassword)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if u.Username != "alice" || !u.IsAdmin() {
		t.Errorf("got %+v, want alice as admin", u)
	}

	// The stored value must be a hash, never the password itself.
	if u.PasswordHash == goodPassword {
		t.Fatal("password stored in plaintext")
	}
	if len(u.PasswordHash) < 50 {
		t.Errorf("password hash %q is too short to be bcrypt", u.PasswordHash)
	}

	if _, err := db.Authenticate(ctx, "alice", "wrong-password-entirely"); !errors.Is(err, ErrNotFound) {
		t.Errorf("wrong password: got %v, want ErrNotFound", err)
	}
}

func TestAuthenticateDoesNotRevealWhetherUserExists(t *testing.T) {
	// A fast "no such user" and a slow "wrong password" turn the login form
	// into a user enumerator. Both paths must run a bcrypt comparison.
	db := testDB(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "alice", goodPassword, domain.RoleUser); err != nil {
		t.Fatal(err)
	}

	timeIt := func(user string) time.Duration {
		start := time.Now()
		_, _ = db.Authenticate(ctx, user, "some-wrong-password")
		return time.Since(start)
	}
	// Warm up, then compare: the unknown user must not be dramatically faster.
	timeIt("alice")
	known := timeIt("alice")
	unknown := timeIt("nobody-here")

	if unknown < known/4 {
		t.Errorf("unknown user took %v vs %v for a known one; the gap leaks existence",
			unknown, known)
	}
	// And the error must be identical either way.
	_, errKnown := db.Authenticate(ctx, "alice", "wrong")
	_, errUnknown := db.Authenticate(ctx, "nobody-here", "wrong")
	if errKnown.Error() != errUnknown.Error() {
		t.Errorf("distinguishable errors: %q vs %q", errKnown, errUnknown)
	}
}

func TestUsernamesAreCaseInsensitiveAndUnique(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	if _, err := db.CreateUser(ctx, "Alice", goodPassword, domain.RoleUser); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateUser(ctx, "alice", goodPassword, domain.RoleUser); !errors.Is(err, ErrDuplicateUsername) {
		t.Errorf("duplicate in different case: got %v, want ErrDuplicateUsername", err)
	}
	if _, err := db.Authenticate(ctx, "ALICE", goodPassword); err != nil {
		t.Errorf("case-insensitive login failed: %v", err)
	}
}

func TestCreateUserValidates(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	cases := []struct{ name, user, pw, role string }{
		{"empty username", "", goodPassword, domain.RoleUser},
		{"whitespace in username", "two words", goodPassword, domain.RoleUser},
		{"short password", "bob", "short", domain.RoleUser},
		{"unknown role", "bob", goodPassword, "superuser"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := db.CreateUser(ctx, c.user, c.pw, c.role); err == nil {
				t.Error("accepted an invalid account")
			}
		})
	}
}

func TestSessionLifecycle(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	id, err := db.CreateUser(ctx, "alice", goodPassword, domain.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.CreateSession(ctx, "token-abc", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	u, err := db.SessionUser(ctx, "token-abc")
	if err != nil {
		t.Fatalf("SessionUser: %v", err)
	}
	if u.Username != "alice" {
		t.Errorf("session resolved to %q", u.Username)
	}

	if err := db.DeleteSession(ctx, "token-abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SessionUser(ctx, "token-abc"); !errors.Is(err, ErrNotFound) {
		t.Error("revoked session still resolves; logging out does not log out")
	}
	if _, err := db.SessionUser(ctx, "never-existed"); !errors.Is(err, ErrNotFound) {
		t.Error("unknown token resolved to a user")
	}
}

func TestExpiredSessionIsRejectedAndCleanedUp(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	id, _ := db.CreateUser(ctx, "alice", goodPassword, domain.RoleUser)

	if err := db.CreateSession(ctx, "stale", id, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SessionUser(ctx, "stale"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired session accepted: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE token = ?`, "stale").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("expired session row was not cleaned up on the way past")
	}
}

func TestChangingPasswordRevokesExistingSessions(t *testing.T) {
	// Changing a password is what people do when they suspect a compromise.
	// Leaving the attacker's session working would defeat the point.
	db := testDB(t)
	ctx := t.Context()
	id, _ := db.CreateUser(ctx, "alice", goodPassword, domain.RoleUser)
	if err := db.CreateSession(ctx, "live-token", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := db.SetPassword(ctx, "alice", "a-brand-new-long-password"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	if _, err := db.SessionUser(ctx, "live-token"); !errors.Is(err, ErrNotFound) {
		t.Error("session survived a password change")
	}
	if _, err := db.Authenticate(ctx, "alice", "a-brand-new-long-password"); err != nil {
		t.Errorf("new password does not work: %v", err)
	}
	if _, err := db.Authenticate(ctx, "alice", goodPassword); err == nil {
		t.Error("old password still works")
	}
}

func TestDeletingUserCascadesToSessions(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	id, _ := db.CreateUser(ctx, "alice", goodPassword, domain.RoleUser)
	if err := db.CreateSession(ctx, "tok", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d sessions outlived their user", n)
	}
}

func TestHasUsersAndList(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()

	has, err := db.HasUsers(ctx)
	if err != nil || has {
		t.Fatalf("HasUsers on an empty database = (%v, %v), want (false, nil)", has, err)
	}
	if _, err := db.CreateUser(ctx, "alice", goodPassword, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if has, _ := db.HasUsers(ctx); !has {
		t.Error("HasUsers = false after creating one")
	}

	users, err := db.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Username != "alice" {
		t.Fatalf("Users = %+v", users)
	}
	// The listing must not carry hashes around.
	if users[0].PasswordHash != "" {
		t.Error("Users returned a password hash")
	}
}

func TestPurgeExpiredSessions(t *testing.T) {
	db := testDB(t)
	ctx := t.Context()
	id, _ := db.CreateUser(ctx, "alice", goodPassword, domain.RoleUser)
	if err := db.CreateSession(ctx, "old", id, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(ctx, "new", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.PurgeExpiredSessions(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d sessions left after purge, want 1", n)
	}
}
