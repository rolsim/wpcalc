package apiv1

import (
	"context"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
)

func (a *API) Healthz(_ context.Context, _ HealthzRequestObject) (HealthzResponseObject, error) {
	return Healthz200JSONResponse{Status: Ok}, nil
}

func (a *API) ListTenants(ctx context.Context, _ ListTenantsRequestObject) (ListTenantsResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageTenants) {
		return ListTenantsdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	tenants, err := a.db.Tenants(ctx)
	if err != nil {
		status, code := mapStoreErr(err)
		return ListTenantsdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	out := make([]Tenant, 0, len(tenants))
	for _, t := range tenants {
		out = append(out, toAPITenant(t))
	}
	return ListTenants200JSONResponse(out), nil
}

// ListAccessibleTenants requires no particular permission beyond being
// authenticated — unlike ListTenants (manage_tenants, system-wide), the
// result here is already scoped to the caller, mirroring
// internal/httpx.accessibleTenants exactly (same "system role sees
// everything, otherwise TenantsAccessibleToUser" rule) so a tenant- or
// employee-scope token can discover which tenantId to use without already
// knowing it.
func (a *API) ListAccessibleTenants(ctx context.Context, _ ListAccessibleTenantsRequestObject) (ListAccessibleTenantsResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok {
		return ListAccessibleTenantsdefaultJSONResponse{Body: Error{Error: codeUnauthorized}, StatusCode: 401}, nil
	}

	var tenants []domain.Tenant
	var err error
	if id.FullAccess || id.CanSystemWide(domain.PermManageTenants) || id.CanSystemWide(domain.PermManageRoles) {
		tenants, err = a.db.Tenants(ctx)
	} else {
		tenants, err = a.db.TenantsAccessibleToUser(ctx, id.UserID)
	}
	if err != nil {
		status, code := mapStoreErr(err)
		return ListAccessibleTenantsdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}

	out := make([]Tenant, 0, len(tenants))
	for _, t := range tenants {
		out = append(out, toAPITenant(t))
	}
	return ListAccessibleTenants200JSONResponse(out), nil
}

func (a *API) CreateTenant(ctx context.Context, request CreateTenantRequestObject) (CreateTenantResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageTenants) {
		return CreateTenantdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return CreateTenantdefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	tenantID, err := a.db.CreateTenant(ctx, request.Body.Name)
	if err != nil {
		status, code := mapStoreErr(err)
		return CreateTenantdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return CreateTenant201JSONResponse{Id: tenantID, Name: request.Body.Name}, nil
}

func (a *API) GetTenant(ctx context.Context, request GetTenantRequestObject) (GetTenantResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageTenants) {
		return GetTenantdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	t, err := a.db.Tenant(ctx, request.TenantId)
	if err != nil {
		status, code := mapStoreErr(err)
		return GetTenantdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return GetTenant200JSONResponse(toAPITenant(t)), nil
}

func (a *API) UpdateTenant(ctx context.Context, request UpdateTenantRequestObject) (UpdateTenantResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanSystemWide(domain.PermManageTenants) {
		return UpdateTenantdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return UpdateTenantdefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	if err := a.db.RenameTenant(ctx, request.TenantId, request.Body.Name); err != nil {
		status, code := mapStoreErr(err)
		return UpdateTenantdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return UpdateTenant200JSONResponse{Id: request.TenantId, Name: request.Body.Name}, nil
}
