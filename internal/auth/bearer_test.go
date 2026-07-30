package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rolsim/wpcalc/internal/domain"
)

type fakeTokenStore struct {
	tokenToUser map[string]domain.User
	roles       map[int64][]domain.UserRole
	perms       map[string][]string
}

func (f *fakeTokenStore) UserByAPIToken(_ context.Context, token string) (domain.User, error) {
	u, ok := f.tokenToUser[token]
	if !ok {
		return domain.User{}, errors.New("not found")
	}
	return u, nil
}

func (f *fakeTokenStore) UserRolesForUser(_ context.Context, userID int64) ([]domain.UserRole, error) {
	return f.roles[userID], nil
}

func (f *fakeTokenStore) RolePermissionsFor(_ context.Context, roleIDs []string) (map[string][]string, error) {
	out := make(map[string][]string, len(roleIDs))
	for _, id := range roleIDs {
		out[id] = f.perms[id]
	}
	return out, nil
}

func TestBearerTokensIdentifyResolvesAValidToken(t *testing.T) {
	store := &fakeTokenStore{
		tokenToUser: map[string]domain.User{"wpat_good": {ID: 1, Username: "alice", Language: "en"}},
		roles:       map[int64][]domain.UserRole{1: {{RoleID: "editor", EmployeeID: intPtr(9)}}},
		perms:       map[string][]string{"editor": {"read", "print", "write"}},
	}
	b := NewBearerTokens(store)

	r := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/1/employees", nil)
	r.Header.Set("Authorization", "Bearer wpat_good")
	id, err := b.Identify(r)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	if id.UserID != 1 || id.Username != "alice" {
		t.Fatalf("identity = %+v", id)
	}
	if id.ActiveTenantID != nil {
		t.Fatalf("bearer identity should never carry an active tenant, got %v", id.ActiveTenantID)
	}
	if !id.Can(domain.PermWrite, 9, 1) {
		t.Fatal("expected the resolved role's permission to apply")
	}
}

func TestBearerTokensIdentifyRejectsMissingHeader(t *testing.T) {
	b := NewBearerTokens(&fakeTokenStore{})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	if _, err := b.Identify(r); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

func TestBearerTokensIdentifyRejectsMalformedHeader(t *testing.T) {
	b := NewBearerTokens(&fakeTokenStore{})
	for _, h := range []string{"", "Bearer", "Bearer ", "Basic dXNlcjpwYXNz", "wpat_good"} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
		if h != "" {
			r.Header.Set("Authorization", h)
		}
		if _, err := b.Identify(r); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("header %q: err = %v, want ErrUnauthenticated", h, err)
		}
	}
}

func TestBearerTokensIdentifyRejectsUnknownToken(t *testing.T) {
	b := NewBearerTokens(&fakeTokenStore{tokenToUser: map[string]domain.User{}})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	r.Header.Set("Authorization", "Bearer wpat_unknown")
	if _, err := b.Identify(r); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
}

func intPtr(v int64) *int64 { return &v }
