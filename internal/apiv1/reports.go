package apiv1

import (
	"bytes"
	"context"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
	"github.com/rolsim/wpcalc/internal/report"
)

func (a *API) GetTenantMonthReport(ctx context.Context, request GetTenantMonthReportRequestObject) (GetTenantMonthReportResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.CanInTenant(domain.PermPrint, request.TenantId) {
		return GetTenantMonthReportdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	month, err := domain.ParseYearMonth(request.Ym)
	if err != nil {
		return GetTenantMonthReportdefaultJSONResponse{Body: Error{Error: "invalid_month"}, StatusCode: 404}, nil //nolint:nilerr
	}
	var buf bytes.Buffer
	r := report.New(a.db, a.printerFor(id.Language))
	if err := r.MonthSummary(ctx, request.TenantId, month, &buf); err != nil {
		status, code := mapStoreErr(err)
		return GetTenantMonthReportdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return GetTenantMonthReport200ApplicationpdfResponse{
		Body:          bytes.NewReader(buf.Bytes()),
		ContentLength: int64(buf.Len()),
	}, nil
}

func (a *API) GetEmployeeMonthReport(ctx context.Context, request GetEmployeeMonthReportRequestObject) (GetEmployeeMonthReportResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.Can(domain.PermPrint, request.EmployeeId, request.TenantId) {
		return GetEmployeeMonthReportdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	month, err := domain.ParseYearMonth(request.Ym)
	if err != nil {
		return GetEmployeeMonthReportdefaultJSONResponse{Body: Error{Error: "invalid_month"}, StatusCode: 404}, nil //nolint:nilerr
	}
	e, err := a.db.Employee(ctx, request.EmployeeId)
	if err != nil {
		status, code := mapStoreErr(err)
		return GetEmployeeMonthReportdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	if e.TenantID != request.TenantId {
		return GetEmployeeMonthReportdefaultJSONResponse{Body: Error{Error: codeNotFound}, StatusCode: 404}, nil
	}
	var buf bytes.Buffer
	r := report.New(a.db, a.printerFor(id.Language))
	if err := r.EmployeeMonth(ctx, request.EmployeeId, month, &buf); err != nil {
		status, code := mapStoreErr(err)
		return GetEmployeeMonthReportdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return GetEmployeeMonthReport200ApplicationpdfResponse{
		Body:          bytes.NewReader(buf.Bytes()),
		ContentLength: int64(buf.Len()),
	}, nil
}

func (a *API) GetEmployeeYearReport(ctx context.Context, request GetEmployeeYearReportRequestObject) (GetEmployeeYearReportResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok || !id.Can(domain.PermPrint, request.EmployeeId, request.TenantId) {
		return GetEmployeeYearReportdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	e, err := a.db.Employee(ctx, request.EmployeeId)
	if err != nil {
		status, code := mapStoreErr(err)
		return GetEmployeeYearReportdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	if e.TenantID != request.TenantId {
		return GetEmployeeYearReportdefaultJSONResponse{Body: Error{Error: codeNotFound}, StatusCode: 404}, nil
	}
	var buf bytes.Buffer
	r := report.New(a.db, a.printerFor(id.Language))
	if err := r.EmployeeYear(ctx, request.EmployeeId, request.Year, &buf); err != nil {
		status, code := mapStoreErr(err)
		return GetEmployeeYearReportdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return GetEmployeeYearReport200ApplicationpdfResponse{
		Body:          bytes.NewReader(buf.Bytes()),
		ContentLength: int64(buf.Len()),
	}, nil
}
