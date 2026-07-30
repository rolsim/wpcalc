package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/rolsim/wpcalc/internal/domain"
)

// TokenStore is the subset of internal/store.DB that BearerTokens needs.
type TokenStore interface {
	UserByAPIToken(ctx context.Context, token string) (domain.User, error)
	UserRolesForUser(ctx context.Context, userID int64) ([]domain.UserRole, error)
	RolePermissionsFor(ctx context.Context, roleIDs []string) (map[string][]string, error)
}

// BearerTokens authenticates /api/v1 requests against the Authorization
// header instead of the wpcalc_session cookie Accounts uses. A bearer
// token has no session to activate a tenant into — unlike the HTML app,
// every API request names its tenant explicitly in the path — so the
// resulting Identity always has a nil ActiveTenantID; callers must check
// CanInTenant/Can against the tenant named in the request, not rely on
// tenant auto-selection the way the HTML handlers do.
type BearerTokens struct {
	store TokenStore
}

// NewBearerTokens builds a BearerTokens authenticator over store.
func NewBearerTokens(store TokenStore) *BearerTokens {
	return &BearerTokens{store: store}
}

var _ Authenticator = (*BearerTokens)(nil)

// Identify implements Authenticator by reading `Authorization: Bearer
// <token>`, resolving it to an account, and loading that account's roles
// and permissions fresh — same no-caching guarantee as Accounts.Identify,
// so a token revoked mid-session stops working on the very next request.
func (b *BearerTokens) Identify(r *http.Request) (Identity, error) {
	token, ok := bearerToken(r)
	if !ok {
		return Identity{}, ErrUnauthenticated
	}

	u, err := b.store.UserByAPIToken(r.Context(), token)
	if err != nil {
		return Identity{}, ErrUnauthenticated
	}

	userRoles, err := b.store.UserRolesForUser(r.Context(), u.ID)
	if err != nil {
		return Identity{}, err
	}
	roleIDs := make([]string, 0, len(userRoles))
	seen := make(map[string]bool, len(userRoles))
	for _, ur := range userRoles {
		if !seen[ur.RoleID] {
			seen[ur.RoleID] = true
			roleIDs = append(roleIDs, ur.RoleID)
		}
	}
	perms, err := b.store.RolePermissionsFor(r.Context(), roleIDs)
	if err != nil {
		return Identity{}, err
	}

	return Identity{
		UserID:          u.ID,
		Username:        u.Username,
		Language:        u.Language,
		UserRoles:       userRoles,
		RolePermissions: perms,
	}, nil
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(h, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}
