package auth

import (
	"context"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/rolsim/wpcalc/internal/domain"
)

// fakeScopedStore is a minimal in-memory ScopedUserStore for exercising the
// ScopeSelf identity path without a real database.
type fakeScopedStore struct {
	users     map[string]domain.User
	userRoles map[uuid.UUID][]domain.UserRole
	perms     map[string][]string
}

func (f *fakeScopedStore) UserByUsername(_ context.Context, username string) (domain.User, error) {
	u, ok := f.users[username]
	if !ok {
		return domain.User{}, errors.New("not found")
	}
	return u, nil
}

func (f *fakeScopedStore) UserRolesForUser(_ context.Context, userID uuid.UUID) ([]domain.UserRole, error) {
	return f.userRoles[userID], nil
}

func (f *fakeScopedStore) RolePermissionsFor(_ context.Context, roleIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(roleIDs))
	for _, id := range roleIDs {
		out[id] = f.perms[id]
	}
	return out, nil
}

// Fixture id for the WordPress-linked account in this file's fakes.
var wpAliceID = uuid.NewV4()

func TestScopeSelfResolvesLinkedAccount(t *testing.T) {
	empID := uuid.NewV4()
	store := &fakeScopedStore{
		users: map[string]domain.User{
			"alice": {ID: wpAliceID, Username: "alice", Language: "de-CH"},
		},
		userRoles: map[uuid.UUID][]domain.UserRole{
			wpAliceID: {{RoleID: "viewer", EmployeeID: &empID}},
		},
		perms: map[string][]string{"viewer": {"read"}},
	}
	a := newWP(t).WithStore(store)

	r := wpScopedRequest(t, a, ConnUnix, "alice", "", ScopeSelf, time.Now())
	id, err := a.Identify(r)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.FullAccess {
		t.Error("ScopeSelf identity must never be FullAccess")
	}
	if id.UserID != wpAliceID || id.Username != "alice" {
		t.Errorf("got %+v, want linked account alice (id 1)", id)
	}
	if !id.Can("read", empID, uuid.NewV4()) {
		t.Error("linked account's employee-scope role did not carry through")
	}
	if id.Can("write", empID, uuid.NewV4()) {
		t.Error("viewer role must not grant write")
	}
}

func TestScopeSelfWithoutMatchingAccountFails(t *testing.T) {
	store := &fakeScopedStore{users: map[string]domain.User{}}
	a := newWP(t).WithStore(store)

	r := wpScopedRequest(t, a, ConnUnix, "mallory", "", ScopeSelf, time.Now())
	if _, err := a.Identify(r); !errors.Is(err, ErrNoLinkedAccount) {
		t.Errorf("Identify = %v, want ErrNoLinkedAccount", err)
	}
}

func TestScopeSelfWithNoRolesFails(t *testing.T) {
	store := &fakeScopedStore{
		users:     map[string]domain.User{"bob": {ID: uuid.NewV4(), Username: "bob"}},
		userRoles: map[uuid.UUID][]domain.UserRole{},
	}
	a := newWP(t).WithStore(store)

	r := wpScopedRequest(t, a, ConnUnix, "bob", "", ScopeSelf, time.Now())
	if _, err := a.Identify(r); !errors.Is(err, ErrNoLinkedAccount) {
		t.Errorf("Identify = %v, want ErrNoLinkedAccount for a linked account with no roles", err)
	}
}

func TestScopeSelfWithoutStoreConfiguredFails(t *testing.T) {
	// WithStore was never called: this is buildAuthenticator wiring the
	// admin-only WordPress authenticator, which must not silently accept
	// ScopeSelf requests it has no way to resolve.
	a := newWP(t)

	r := wpScopedRequest(t, a, ConnUnix, "alice", "", ScopeSelf, time.Now())
	if _, err := a.Identify(r); !errors.Is(err, ErrNoLinkedAccount) {
		t.Errorf("Identify = %v, want ErrNoLinkedAccount", err)
	}
}

func TestScopeAdminUnaffectedByScopeSelfSupport(t *testing.T) {
	// Adding store-backed ScopeSelf support must not change ScopeAdmin
	// behavior for a WordPress authenticator that has one configured.
	empID := uuid.NewV4()
	store := &fakeScopedStore{
		users: map[string]domain.User{"alice": {ID: wpAliceID, Username: "alice"}},
		userRoles: map[uuid.UUID][]domain.UserRole{
			wpAliceID: {{RoleID: "viewer", EmployeeID: &empID}},
		},
		perms: map[string][]string{"viewer": {"read"}},
	}
	a := newWP(t).WithStore(store)

	r := wpScopedRequest(t, a, ConnUnix, "alice", "administrator", ScopeAdmin, time.Now())
	id, err := a.Identify(r)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if !id.FullAccess || id.Username != "alice" {
		t.Errorf("got %+v, want FullAccess alice", id)
	}
}

func TestScopeCannotBeDowngradedByStrippingHeader(t *testing.T) {
	// A signature produced for ScopeSelf must not verify as ScopeAdmin (or
	// vice versa) if the X-Wpcalc-Scope header is edited or removed after
	// signing — scope is bound into the mac.
	store := &fakeScopedStore{
		users:     map[string]domain.User{"alice": {ID: wpAliceID, Username: "alice"}},
		userRoles: map[uuid.UUID][]domain.UserRole{wpAliceID: {{RoleID: "viewer"}}},
		perms:     map[string][]string{"viewer": {"read"}},
	}
	a := newWP(t).WithStore(store)

	r := wpScopedRequest(t, a, ConnUnix, "alice", "", ScopeSelf, time.Now())
	r.Header.Del(HeaderScope) // now looks like a defaulted ScopeAdmin request
	if id, err := a.Identify(r); err == nil {
		t.Errorf("stripped-scope request accepted as %+v", id)
	}
}
