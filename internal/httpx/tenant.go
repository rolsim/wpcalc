package httpx

import (
	"context"
	"net/http"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
)

// accessibleTenants lists the tenants an identity may act in at all — every
// tenant for a FullAccess (WordPress-mode) identity or one holding any
// system-scope role, otherwise just the ones reached via a tenant-scope or
// employee-scope role.
func (s *Server) accessibleTenants(ctx context.Context, id auth.Identity) ([]domain.Tenant, error) {
	if id.FullAccess || id.CanSystemWide(domain.PermManageTenants) || id.CanSystemWide(domain.PermManageRoles) {
		return s.db.Tenants(ctx)
	}
	return s.db.TenantsAccessibleToUser(ctx, id.UserID)
}

// resolveActiveTenant determines which tenant the current request acts in,
// rendering the appropriate response itself when it cannot proceed —
// callers stop and return immediately when ok is false.
//
// RBAC96 models a session as activating a subset of a user's assigned
// roles; this is that activation adapted to tenant scoping. The active
// choice is cached on the session (standalone) so it need not be re-resolved
// every request, but a stale or revoked choice is not specially detected
// here — the permission checks downstream simply deny what it no longer
// covers, which is enough: this function only ever narrows what a request
// can reach, never widens it.
func (s *Server) resolveActiveTenant(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, _ := auth.IdentityFrom(r.Context())
	if id.ActiveTenantID != nil {
		return *id.ActiveTenantID, true
	}

	tenants, err := s.accessibleTenants(r.Context(), id)
	if err != nil {
		s.log.Error("resolve active tenant", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return 0, false
	}

	switch len(tenants) {
	case 0:
		s.renderError(w, r, http.StatusForbidden, "error.no_tenant_access")
		return 0, false
	case 1:
		// Auto-selected and persisted where possible, so the next request
		// does not repeat this lookup.
		if tw, ok := s.authn.(auth.TenantWriter); ok {
			_ = tw.SetActiveTenant(r, &tenants[0].ID)
		}
		return tenants[0].ID, true
	default:
		http.Redirect(w, r, s.url(r, "/tenants/choose"), http.StatusSeeOther)
		return 0, false
	}
}
