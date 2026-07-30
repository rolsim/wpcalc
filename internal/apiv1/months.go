package apiv1

import (
	"context"
	"strconv"

	"github.com/rolsim/wpcalc/internal/auth"
	"github.com/rolsim/wpcalc/internal/domain"
)

const decimalSep = "."

// GetMonthGrid mirrors buildGridView's visibility rule: an employee the
// caller holds no permission on at all is omitted from Employees entirely,
// not merely locked; among the rest, a cell is locked unless the caller can
// write that employee's hours (in addition to the employment-period lock).
func (a *API) GetMonthGrid(ctx context.Context, request GetMonthGridRequestObject) (GetMonthGridResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok {
		return GetMonthGriddefaultJSONResponse{Body: Error{Error: codeUnauthorized}, StatusCode: 401}, nil
	}
	month, err := domain.ParseYearMonth(request.Ym)
	if err != nil {
		// oapi-codegen strict-server: a domain error becomes a typed JSON
		// response body, not a transport-level error, so nil here is correct.
		return GetMonthGriddefaultJSONResponse{Body: Error{Error: "invalid_month"}, StatusCode: 404}, nil //nolint:nilerr
	}

	all, err := a.db.EmployeesActiveIn(ctx, request.TenantId, month)
	if err != nil {
		status, code := mapStoreErr(err)
		return GetMonthGriddefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	employees := make([]domain.Employee, 0, len(all))
	for _, e := range all {
		if id.Can(domain.PermRead, e.ID, request.TenantId) {
			employees = append(employees, e)
		}
	}

	entries, err := a.db.MonthEntries(ctx, request.TenantId, month)
	if err != nil {
		status, code := mapStoreErr(err)
		return GetMonthGriddefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	comments, err := a.db.DayComments(ctx, request.TenantId, month)
	if err != nil {
		status, code := mapStoreErr(err)
		return GetMonthGriddefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	totals, err := a.db.Totals(ctx, request.TenantId, month)
	if err != nil {
		status, code := mapStoreErr(err)
		return GetMonthGriddefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}

	grid := MonthGrid{
		Employees:      make([]Employee, 0, len(employees)),
		Days:           make([]GridDay, 0, len(month.Days())),
		EmployeeTotals: make(map[string]string, len(employees)),
		GrandTotal:     totals.Grand.Format(decimalSep),
	}
	for _, e := range employees {
		grid.Employees = append(grid.Employees, toAPIEmployee(e))
		grid.EmployeeTotals[strconv.FormatInt(e.ID, 10)] = totals.PerEmployee[e.ID].Format(decimalSep)
	}
	for _, day := range month.Days() {
		gd := GridDay{
			Date:  toAPIDate(day),
			Cells: make([]GridCell, 0, len(employees)),
			Total: totals.PerDay[day].Format(decimalSep),
		}
		if c, ok := comments[day]; ok && c != "" {
			gd.Comment = &c
		}
		for _, e := range employees {
			locked := !e.Employed(day) || !id.Can(domain.PermWrite, e.ID, request.TenantId)
			gd.Cells = append(gd.Cells, GridCell{
				EmployeeId: e.ID,
				Hours:      entries[e.ID][day].Format(decimalSep),
				Locked:     locked,
			})
		}
		grid.Days = append(grid.Days, gd)
	}

	return GetMonthGrid200JSONResponse(grid), nil
}

func (a *API) SetHours(ctx context.Context, request SetHoursRequestObject) (SetHoursResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok {
		return SetHoursdefaultJSONResponse{Body: Error{Error: codeUnauthorized}, StatusCode: 401}, nil
	}
	month, err := domain.ParseYearMonth(request.Ym)
	if err != nil {
		return SetHoursdefaultJSONResponse{Body: Error{Error: "invalid_month"}, StatusCode: 404}, nil //nolint:nilerr
	}
	if request.Body == nil {
		return SetHoursdefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	if !id.Can(domain.PermWrite, request.Body.EmployeeId, request.TenantId) {
		return SetHoursdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}

	hours, err := domain.ParseHours(request.Body.Hours)
	if err != nil {
		return SetHoursdefaultJSONResponse{Body: Error{Error: "invalid_hours"}, StatusCode: 422}, nil //nolint:nilerr
	}
	day := fromAPIDate(request.Body.Date)
	if err := a.db.SetHours(ctx, request.Body.EmployeeId, day, hours); err != nil {
		status, code := mapStoreErr(err)
		return SetHoursdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}

	totals, err := a.db.Totals(ctx, request.TenantId, month)
	if err != nil {
		status, code := mapStoreErr(err)
		return SetHoursdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return SetHours200JSONResponse{
		EmployeeTotal: totals.PerEmployee[request.Body.EmployeeId].Format(decimalSep),
		DayTotal:      totals.PerDay[day].Format(decimalSep),
		GrandTotal:    totals.Grand.Format(decimalSep),
	}, nil
}

func (a *API) SetComment(ctx context.Context, request SetCommentRequestObject) (SetCommentResponseObject, error) {
	id, ok := auth.IdentityFrom(ctx)
	if !ok {
		return SetCommentdefaultJSONResponse{Body: Error{Error: codeUnauthorized}, StatusCode: 401}, nil
	}
	if _, err := domain.ParseYearMonth(request.Ym); err != nil {
		return SetCommentdefaultJSONResponse{Body: Error{Error: "invalid_month"}, StatusCode: 404}, nil //nolint:nilerr
	}
	if request.Body == nil {
		return SetCommentdefaultJSONResponse{Body: Error{Error: codeBadRequest}, StatusCode: 400}, nil
	}
	// The day comment is shared, tenant-wide state — not any one employee's
	// — so it takes CanInTenant(write), matching the HTML app's
	// handleSetComment exactly (see internal/httpx/handlers_grid.go).
	if !id.CanInTenant(domain.PermWrite, request.TenantId) {
		return SetCommentdefaultJSONResponse{Body: Error{Error: codeForbidden}, StatusCode: 403}, nil
	}
	comment := ""
	if request.Body.Comment != nil {
		comment = *request.Body.Comment
	}
	day := fromAPIDate(request.Body.Date)
	if err := a.db.SetDayComment(ctx, request.TenantId, day, comment); err != nil {
		status, code := mapStoreErr(err)
		return SetCommentdefaultJSONResponse{Body: Error{Error: code}, StatusCode: status}, nil
	}
	return SetComment204Response{}, nil
}
