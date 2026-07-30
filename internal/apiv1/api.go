package apiv1

import (
	"log/slog"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/i18n"
	"github.com/rolsim/wpcalc/internal/store"
)

// API implements StrictServerInterface against the same store and report
// packages the HTML app uses — /api/v1 is a second front end onto the same
// data and the same RBAC96 checks, not a separate application.
type API struct {
	db     *store.DB
	bundle *i18n.Bundle
	log    *slog.Logger
}

// New builds an API.
func New(db *store.DB, bundle *i18n.Bundle, log *slog.Logger) *API {
	return &API{db: db, bundle: bundle, log: log}
}

var _ StrictServerInterface = (*API)(nil)

func toAPIDate(d domain.Date) openapi_types.Date {
	return openapi_types.Date{Time: d.Time()}
}

func fromAPIDate(d openapi_types.Date) domain.Date {
	return domain.DateOf(d.Time)
}

func optAPIDate(d *domain.Date) *openapi_types.Date {
	if d == nil {
		return nil
	}
	v := toAPIDate(*d)
	return &v
}

func optDomainDate(d *openapi_types.Date) *domain.Date {
	if d == nil {
		return nil
	}
	v := fromAPIDate(*d)
	return &v
}

func toAPITenant(t domain.Tenant) Tenant {
	return Tenant{Id: t.ID, Name: t.Name}
}

func toAPIEmployee(e domain.Employee) Employee {
	return Employee{
		Id:        e.ID,
		TenantId:  e.TenantID,
		Name:      e.DisplayName,
		StartDate: toAPIDate(e.StartDate),
		EndDate:   optAPIDate(e.EndDate),
	}
}

func toAPIPermission(p domain.Permission) Permission {
	return Permission{Id: PermissionKey(p.ID), MinScope: Scope(p.MinScope)}
}

func toAPIRole(r domain.Role, permissionIDs []string) Role {
	perms := make([]PermissionKey, 0, len(permissionIDs))
	for _, id := range permissionIDs {
		perms = append(perms, PermissionKey(id))
	}
	return Role{Id: r.ID, Name: r.Name, Scope: Scope(r.Scope), Permissions: perms}
}

func toAPIAdminAssignment(a store.AdminRoleAssignment) AdminRoleAssignment {
	out := AdminRoleAssignment{
		UserId:   a.UserID,
		Username: a.Username,
		RoleId:   a.RoleID,
		RoleName: a.RoleName,
		TenantId: a.TenantID,
	}
	if a.TenantID != nil {
		name := a.TenantName
		out.TenantName = &name
	}
	return out
}

func toAPIEmployeeAssignment(a store.EmployeeRoleAssignment) EmployeeRoleAssignment {
	return EmployeeRoleAssignment{
		UserId:       a.UserID,
		Username:     a.Username,
		EmployeeId:   a.EmployeeID,
		EmployeeName: a.EmployeeName,
		RoleId:       a.RoleID,
		RoleName:     a.RoleName,
	}
}

func (a *API) printerFor(lang string) *i18n.Printer {
	return a.bundle.For(lang)
}
