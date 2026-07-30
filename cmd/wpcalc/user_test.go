package main

import "testing"

func TestScopeTarget(t *testing.T) {
	cases := []struct {
		name             string
		system           bool
		tenant, employee int64
		wantErr          bool
		wantTenant       bool
		wantEmployee     bool
	}{
		{"system", true, 0, 0, false, false, false},
		{"tenant", false, 5, 0, false, true, false},
		{"employee", false, 0, 7, false, false, true},
		{"none set", false, 0, 0, true, false, false},
		{"two set", true, 5, 0, true, false, false},
		{"all three set", true, 5, 7, true, false, false},
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
