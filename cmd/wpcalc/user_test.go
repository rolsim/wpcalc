package main

import (
	"testing"
	"uuid"
)

func TestScopeTarget(t *testing.T) {
	// Scope targets arrive as flag strings now, so "not set" is "" rather than
	// 0, and a set value has to be a parseable UUID — which gives this table a
	// case it could not have before: a flag that is present but malformed.
	someTenant := uuid.NewV4().String()
	someEmployee := uuid.NewV4().String()

	cases := []struct {
		name             string
		system           bool
		tenant, employee string
		wantErr          bool
		wantTenant       bool
		wantEmployee     bool
	}{
		{"system", true, "", "", false, false, false},
		{"tenant", false, someTenant, "", false, true, false},
		{"employee", false, "", someEmployee, false, false, true},
		{"none set", false, "", "", true, false, false},
		{"two set", true, someTenant, "", true, false, false},
		{"all three set", true, someTenant, someEmployee, true, false, false},
		{"tenant not a uuid", false, "5", "", true, false, false},
		{"employee not a uuid", false, "", "not-a-uuid", true, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tenantID, employeeID, err := scopeTarget(c.system, c.tenant, c.employee)
			if (err != nil) != c.wantErr {
				t.Fatalf("scopeTarget error = %v, wantErr %v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if (tenantID != nil) != c.wantTenant {
				t.Errorf("tenantID = %v, wantTenant %v", tenantID, c.wantTenant)
			}
			if (employeeID != nil) != c.wantEmployee {
				t.Errorf("employeeID = %v, wantEmployee %v", employeeID, c.wantEmployee)
			}
		})
	}
}
