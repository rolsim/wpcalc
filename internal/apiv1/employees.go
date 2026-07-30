package apiv1

import (
	"context"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
)

func (a *API) ListEmployees(ctx context.Context, request ListEmployeesRequestObject) (ListEmployeesResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanInTenant(domain.PermManageEmployees, request.TenantId) {
		return ListEmployeesdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	employees, err := a.db.Employees(ctx, request.TenantId)
	if err != nil {
		status, code := mapStoreErr(err)
		return ListEmployeesdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	out := make([]Employee, 0, len(employees))
	for _, e := range employees {
		out = append(out, toAPIEmployee(e))
	}
	return ListEmployees200JSONResponse(out), nil
}

func (a *API) CreateEmployee(ctx context.Context, request CreateEmployeeRequestObject) (CreateEmployeeResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanInTenant(domain.PermManageEmployees, request.TenantId) {
		return CreateEmployeedefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return CreateEmployeedefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	e := domain.Employee{
		TenantID:    request.TenantId,
		DisplayName: request.Body.Name,
		StartDate:   fromAPIDate(request.Body.StartDate),
		EndDate:     optDomainDate(request.Body.EndDate),
	}
	empID, err := a.db.CreateEmployee(ctx, e)
	if err != nil {
		status, code := mapStoreErr(err)
		return CreateEmployeedefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	e.ID = empID
	return CreateEmployee201JSONResponse(toAPIEmployee(e)), nil
}

func (a *API) GetEmployee(ctx context.Context, request GetEmployeeRequestObject) (GetEmployeeResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanInTenant(domain.PermManageEmployees, request.TenantId) {
		return GetEmployeedefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	e, err := a.db.Employee(ctx, request.EmployeeId)
	if err != nil {
		status, code := mapStoreErr(err)
		return GetEmployeedefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	if e.TenantID != request.TenantId {
		return GetEmployeedefaultJSONResponse{Body: Error{Error: codeNotFound}, StatusCode: 404}, nil
	}
	return GetEmployee200JSONResponse(toAPIEmployee(e)), nil
}

func (a *API) UpdateEmployee(ctx context.Context, request UpdateEmployeeRequestObject) (UpdateEmployeeResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanInTenant(domain.PermManageEmployees, request.TenantId) {
		return UpdateEmployeedefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	if request.Body == nil {
		return UpdateEmployeedefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	existing, err := a.db.Employee(ctx, request.EmployeeId)
	if err != nil {
		status, code := mapStoreErr(err)
		return UpdateEmployeedefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	if existing.TenantID != request.TenantId {
		return UpdateEmployeedefaultJSONResponse{Body: Error{Error: codeNotFound}, StatusCode: 404}, nil
	}
	updated := domain.Employee{
		ID:          request.EmployeeId,
		TenantID:    existing.TenantID,
		DisplayName: request.Body.Name,
		StartDate:   fromAPIDate(request.Body.StartDate),
		EndDate:     optDomainDate(request.Body.EndDate),
	}
	if err := a.db.UpdateEmployee(ctx, updated); err != nil {
		status, code := mapStoreErr(err)
		return UpdateEmployeedefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return UpdateEmployee200JSONResponse(toAPIEmployee(updated)), nil
}

func (a *API) DeleteEmployee(ctx context.Context, request DeleteEmployeeRequestObject) (DeleteEmployeeResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanInTenant(domain.PermManageEmployees, request.TenantId) {
		return DeleteEmployeedefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	existing, err := a.db.Employee(ctx, request.EmployeeId)
	if err != nil {
		status, code := mapStoreErr(err)
		return DeleteEmployeedefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	if existing.TenantID != request.TenantId {
		return DeleteEmployeedefaultJSONResponse{Body: Error{Error: codeNotFound}, StatusCode: 404}, nil
	}
	if err := a.db.DeleteEmployee(ctx, request.EmployeeId); err != nil {
		status, code := mapStoreErr(err)
		return DeleteEmployeedefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return DeleteEmployee204Response{}, nil
}
