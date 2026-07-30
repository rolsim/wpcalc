package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Tenant is one company ("Mandant") whose employees, users, and hours are
// isolated from every other tenant sharing the same database.
//
// This is not part of core RBAC96 — the standard has no concept of
// multi-tenancy — it is the resource hierarchy that UserRole (see rbac.go)
// is scoped against.
type Tenant struct {
	ID   int64
	Name string
}

// ErrInvalidTenant is the sentinel for tenant validation failures.
var ErrInvalidTenant = errors.New("invalid tenant")

// ValidTenantName checks a candidate tenant name.
func ValidTenantName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidTenant)
	}
	return nil
}
