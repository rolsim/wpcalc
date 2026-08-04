package httpx

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
)

// handleTenantChoose shows the tenant switcher as a full page, for the
// moment right after login when the account has several accessible tenants
// and none is yet active — resolveActiveTenant redirects here rather than
// guessing.
func (s *Server) handleTenantChoose(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	tenants, err := s.accessibleTenants(r.Context(), id)
	if err != nil {
		s.log.Error("list accessible tenants", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}
	if len(tenants) <= 1 {
		http.Redirect(w, r, s.url(r, "/"), http.StatusSeeOther)
		return
	}

	v := struct {
		view
		Tenants []domain.Tenant
	}{view: s.newView(r, "tenant.choose"), Tenants: tenants}
	s.render(w, r, "tenant_choose.html", http.StatusOK, v)
}

// handleTenantSwitch activates a tenant for the current session — RBAC96
// session role-activation, adapted to tenant scoping.
func (s *Server) handleTenantSwitch(w http.ResponseWriter, r *http.Request) {
	if err := parseAnyForm(r); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "error.invalid_input")
		return
	}
	tenantID, err := strconv.ParseInt(r.PostFormValue("tenant_id"), 10, 64)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "error.invalid_input")
		return
	}

	id, _ := auth.IdentityFrom(r.Context())
	tenants, err := s.accessibleTenants(r.Context(), id)
	if err != nil {
		s.log.Error("switch tenant", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}
	allowed := false
	for _, t := range tenants {
		if t.ID == tenantID {
			allowed = true
			break
		}
	}
	if !allowed {
		s.renderError(w, r, http.StatusForbidden, "error.forbidden")
		return
	}

	if tw, ok := s.authn.(auth.TenantWriter); ok {
		if err := tw.SetActiveTenant(r, &tenantID); err != nil {
			s.log.Error("switch tenant", "error", err)
			s.renderError(w, r, http.StatusInternalServerError, "error.server")
			return
		}
	}

	returnTo := r.PostFormValue("return_to")
	if returnTo == "" {
		returnTo = s.url(r, "/")
	}
	http.Redirect(w, r, returnTo, http.StatusSeeOther)
}

type tenantListView struct {
	view
	Tenants []domain.Tenant
}

// handleTenantList is the super-admin (manage_tenants) page: every tenant,
// plus a form to create one.
func (s *Server) handleTenantList(w http.ResponseWriter, r *http.Request) {
	tenants, err := s.db.Tenants(r.Context())
	if err != nil {
		s.log.Error("list tenants", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}
	v := tenantListView{view: s.newView(r, "tenant.heading"), Tenants: tenants}
	if key := r.URL.Query().Get("err"); key != "" {
		v.Error = v.T(errorKey(key))
	}
	s.render(w, r, "tenants.html", http.StatusOK, v)
}

func (s *Server) handleTenantCreate(w http.ResponseWriter, r *http.Request) {
	if err := parseAnyForm(r); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "error.invalid_input")
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if _, err := s.db.CreateTenant(r.Context(), name); err != nil {
		s.log.Warn("create tenant", "error", err)
		http.Redirect(w, r, s.url(r, "/tenants?err=invalid_input"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, s.url(r, "/tenants"), http.StatusSeeOther)
}

type tenantAccessView struct {
	view
	Tenant      domain.Tenant
	Employees   []domain.Employee
	Roles       []domain.Role
	Assignments []employeeAssignmentRow
}

type employeeAssignmentRow struct {
	Username     string
	EmployeeName string
	RoleName     string
	UserID       int64
	EmployeeID   int64
}

// handleTenantAccess is the per-tenant page for granting/revoking
// employee-scope roles (viewer/reporter/editor, or any custom employee-scope
// role) — manage_users within this tenant, held by its mandant-admin or a
// system-wide manage_roles/manage_users holder. It deliberately cannot touch
// tenant- or system-scope assignments: a mandant-admin minting more
// mandant-admins, or demoting the one auditing them, is the wrong trust
// boundary — that lives on /roles instead.
func (s *Server) handleTenantAccess(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantIDFromPath(w, r)
	if !ok {
		return
	}
	id, _ := auth.IdentityFrom(r.Context())
	if !id.CanInTenant(domain.PermManageUsers, tenantID) {
		s.renderError(w, r, http.StatusForbidden, "error.forbidden")
		return
	}

	tenant, err := s.db.Tenant(r.Context(), tenantID)
	if err != nil {
		s.tenantLookupError(w, r, err)
		return
	}
	employees, err := s.db.Employees(r.Context(), tenantID)
	if err != nil {
		s.log.Error("tenant access", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}
	roles, err := s.db.Roles(r.Context())
	if err != nil {
		s.log.Error("tenant access", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}
	employeeRoles := make([]domain.Role, 0, len(roles))
	for _, role := range roles {
		if role.Scope == domain.ScopeEmployee {
			employeeRoles = append(employeeRoles, role)
		}
	}
	assignments, err := s.db.EmployeeRoleAssignmentsForTenant(r.Context(), tenantID)
	if err != nil {
		s.log.Error("tenant access", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "error.server")
		return
	}

	base := s.newView(r, "tenant.access")
	base.TenantID = tenantID
	v := tenantAccessView{view: base, Tenant: tenant, Employees: employees, Roles: employeeRoles}
	for _, a := range assignments {
		v.Assignments = append(v.Assignments, employeeAssignmentRow{
			Username: a.Username, EmployeeName: a.EmployeeName, RoleName: a.RoleName,
			UserID: a.UserID, EmployeeID: a.EmployeeID,
		})
	}
	if key := r.URL.Query().Get("err"); key != "" {
		v.Error = v.T(errorKey(key))
	}
	s.render(w, r, "tenant_access.html", http.StatusOK, v)
}

// handleTenantAccessGrant assigns an employee-scope role to a user for one
// employee in this tenant.
func (s *Server) handleTenantAccessGrant(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantIDFromPath(w, r)
	if !ok {
		return
	}
	id, _ := auth.IdentityFrom(r.Context())
	if !id.CanInTenant(domain.PermManageUsers, tenantID) {
		s.renderError(w, r, http.StatusForbidden, "error.forbidden")
		return
	}
	if err := parseAnyForm(r); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "error.invalid_input")
		return
	}

	username := strings.TrimSpace(r.PostFormValue("username"))
	employeeID, errE := strconv.ParseInt(r.PostFormValue("employee_id"), 10, 64)
	roleID := strings.TrimSpace(r.PostFormValue("role_id"))
	target, errU := s.db.UserByUsername(r.Context(), username)
	if errE != nil || errU != nil || roleID == "" {
		s.redirectTenantAccessErr(w, r, tenantID, "invalid_input")
		return
	}
	if emp, err := s.db.Employee(r.Context(), employeeID); err != nil || emp.TenantID != tenantID {
		s.redirectTenantAccessErr(w, r, tenantID, "not_found")
		return
	}
	if err := s.db.GrantUserRole(r.Context(), target.ID, nil, &employeeID, roleID); err != nil {
		s.log.Warn("grant employee role", "error", err)
		s.redirectTenantAccessErr(w, r, tenantID, "invalid_input")
		return
	}
	http.Redirect(w, r, s.url(r, "/tenants/%d/access", tenantID), http.StatusSeeOther)
}

// handleTenantAccessRevoke removes a user's employee-scope role in this
// tenant.
func (s *Server) handleTenantAccessRevoke(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantIDFromPath(w, r)
	if !ok {
		return
	}
	id, _ := auth.IdentityFrom(r.Context())
	if !id.CanInTenant(domain.PermManageUsers, tenantID) {
		s.renderError(w, r, http.StatusForbidden, "error.forbidden")
		return
	}
	if err := parseAnyForm(r); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "error.invalid_input")
		return
	}
	userID, errU := strconv.ParseInt(r.PostFormValue("user_id"), 10, 64)
	employeeID, errE := strconv.ParseInt(r.PostFormValue("employee_id"), 10, 64)
	if errU != nil || errE != nil {
		s.redirectTenantAccessErr(w, r, tenantID, "invalid_input")
		return
	}
	if emp, err := s.db.Employee(r.Context(), employeeID); err != nil || emp.TenantID != tenantID {
		s.redirectTenantAccessErr(w, r, tenantID, "not_found")
		return
	}
	if err := s.db.RevokeUserRole(r.Context(), userID, nil, &employeeID); err != nil {
		s.log.Warn("revoke employee role", "error", err)
		s.redirectTenantAccessErr(w, r, tenantID, "not_found")
		return
	}
	http.Redirect(w, r, s.url(r, "/tenants/%d/access", tenantID), http.StatusSeeOther)
}

func (s *Server) redirectTenantAccessErr(w http.ResponseWriter, r *http.Request, tenantID int64, errKey string) {
	http.Redirect(w, r, s.url(r, "/tenants/%d/access?err=%s", tenantID, errKey), http.StatusSeeOther)
}

func (s *Server) tenantIDFromPath(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		s.renderError(w, r, http.StatusNotFound, "error.not_found")
		return 0, false
	}
	return id, true
}

func (s *Server) tenantLookupError(w http.ResponseWriter, r *http.Request, err error) {
	s.employeeLookupError(w, r, err) // same shape: ErrNotFound -> 404, else 500
}
