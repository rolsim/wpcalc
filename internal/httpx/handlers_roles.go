package httpx

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/store"
)

type roleRow struct {
	domain.Role
	Permissions []string
}

type rolesView struct {
	view
	Roles       []roleRow
	Permissions []domain.Permission
	Tenants     []domain.Tenant
	Assignments []store.AdminRoleAssignment
}

// handleRoleList is the super-admin (manage_roles) page: the full role
// catalog with its permissions, and every system- or tenant-scope
// assignment (who is super-admin, who is mandant-admin of which tenant).
// This is the only page that can create another super-admin or
// mandant-admin — employee-scope grants live on /tenants/{id}/access
// instead, reachable by a mandant-admin who must not also be able to mint
// more admins.
func (s *Server) handleRoleList(w http.ResponseWriter, r *http.Request) {
	roles, err := s.db.Roles(r.Context())
	if err != nil {
		s.log.Error("list roles", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}
	roleIDs := make([]string, len(roles))
	for i, ro := range roles {
		roleIDs[i] = ro.ID
	}
	perms, err := s.db.RolePermissionsFor(r.Context(), roleIDs)
	if err != nil {
		s.log.Error("list roles", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}
	permissions, err := s.db.Permissions(r.Context())
	if err != nil {
		s.log.Error("list roles", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}
	tenants, err := s.db.Tenants(r.Context())
	if err != nil {
		s.log.Error("list roles", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}
	assignments, err := s.db.AdminRoleAssignments(r.Context())
	if err != nil {
		s.log.Error("list roles", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}

	v := rolesView{
		view:        s.newView(r, "role.heading"),
		Permissions: permissions,
		Tenants:     tenants,
		Assignments: assignments,
	}
	for _, ro := range roles {
		v.Roles = append(v.Roles, roleRow{Role: ro, Permissions: perms[ro.ID]})
	}
	if key := r.URL.Query().Get("err"); key != "" {
		v.Error = v.T(errorKey(key))
	}
	s.render(w, r, "roles.html", http.StatusOK, v)
}

func (s *Server) handleRoleCreate(w http.ResponseWriter, r *http.Request) {
	if err := parseAnyForm(r); err != nil {
		s.redirectRolesErr(w, r)
		return
	}
	id := strings.TrimSpace(r.PostFormValue("id"))
	name := strings.TrimSpace(r.PostFormValue("name"))
	scope := domain.Scope(r.PostFormValue("scope"))
	if err := s.db.CreateRole(r.Context(), id, name, scope); err != nil {
		s.log.Warn("create role", "error", err)
		s.redirectRolesErr(w, r)
		return
	}
	http.Redirect(w, r, s.url("/roles"), http.StatusSeeOther)
}

func (s *Server) handleRoleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.db.DeleteRole(r.Context(), id); err != nil {
		s.log.Warn("delete role", "error", err)
		s.redirectRolesErr(w, r)
		return
	}
	http.Redirect(w, r, s.url("/roles"), http.StatusSeeOther)
}

// handleRolePermissionAdd grants a role a permission.
func (s *Server) handleRolePermissionAdd(w http.ResponseWriter, r *http.Request) {
	roleID := r.PathValue("id")
	if err := parseAnyForm(r); err != nil {
		s.redirectRolesErr(w, r)
		return
	}
	permissionID := strings.TrimSpace(r.PostFormValue("permission_id"))
	if err := s.db.AddRolePermission(r.Context(), roleID, permissionID); err != nil {
		s.log.Warn("add role permission", "error", err)
		s.redirectRolesErr(w, r)
		return
	}
	http.Redirect(w, r, s.url("/roles"), http.StatusSeeOther)
}

// handleRolePermissionRemove revokes a permission from a role.
func (s *Server) handleRolePermissionRemove(w http.ResponseWriter, r *http.Request) {
	roleID := r.PathValue("id")
	if err := parseAnyForm(r); err != nil {
		s.redirectRolesErr(w, r)
		return
	}
	permissionID := strings.TrimSpace(r.PostFormValue("permission_id"))
	if err := s.db.RemoveRolePermission(r.Context(), roleID, permissionID); err != nil {
		s.log.Warn("remove role permission", "error", err)
		s.redirectRolesErr(w, r)
		return
	}
	http.Redirect(w, r, s.url("/roles"), http.StatusSeeOther)
}

// handleRoleAssign grants a system- or tenant-scope role to a user: a
// tenant_id in the form means tenant scope, its absence system scope.
func (s *Server) handleRoleAssign(w http.ResponseWriter, r *http.Request) {
	if err := parseAnyForm(r); err != nil {
		s.redirectRolesErr(w, r)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	roleID := strings.TrimSpace(r.PostFormValue("role_id"))
	target, err := s.db.UserByUsername(r.Context(), username)
	if err != nil || roleID == "" {
		s.redirectRolesErr(w, r)
		return
	}

	var tenantID *int64
	if raw := strings.TrimSpace(r.PostFormValue("tenant_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			s.redirectRolesErr(w, r)
			return
		}
		tenantID = &id
	}

	if err := s.db.GrantUserRole(r.Context(), target.ID, tenantID, nil, roleID); err != nil {
		s.log.Warn("grant admin role", "error", err)
		s.redirectRolesErr(w, r)
		return
	}
	http.Redirect(w, r, s.url("/roles"), http.StatusSeeOther)
}

// handleRoleRevoke removes a user's system- or tenant-scope role.
func (s *Server) handleRoleRevoke(w http.ResponseWriter, r *http.Request) {
	if err := parseAnyForm(r); err != nil {
		s.redirectRolesErr(w, r)
		return
	}
	userID, err := strconv.ParseInt(r.PostFormValue("user_id"), 10, 64)
	if err != nil {
		s.redirectRolesErr(w, r)
		return
	}
	var tenantID *int64
	if raw := strings.TrimSpace(r.PostFormValue("tenant_id")); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			s.redirectRolesErr(w, r)
			return
		}
		tenantID = &id
	}
	if err := s.db.RevokeUserRole(r.Context(), userID, tenantID, nil); err != nil {
		s.log.Warn("revoke admin role", "error", err)
		s.redirectRolesErr(w, r)
		return
	}
	http.Redirect(w, r, s.url("/roles"), http.StatusSeeOther)
}

func (s *Server) redirectRolesErr(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, s.url("/roles?err=invalid_input"), http.StatusSeeOther)
}
