package domain

import "testing"

func TestRoleCapabilityMatrix(t *testing.T) {
	t.Parallel()

	capabilities := []Capability{
		CapabilityMembersManage,
		CapabilityProvidersManage,
		CapabilityDocumentsProcess,
		CapabilityClaimsReview,
		CapabilityReviewSourceRead,
		CapabilityFactsRead,
		CapabilityAllocationsManage,
		CapabilityResourcesDelete,
	}
	allowed := map[Role]map[Capability]bool{
		RoleOwner: {
			CapabilityMembersManage: true, CapabilityProvidersManage: true,
			CapabilityDocumentsProcess: true, CapabilityClaimsReview: true,
			CapabilityReviewSourceRead: true, CapabilityFactsRead: true,
			CapabilityAllocationsManage: true,
			CapabilityResourcesDelete:   true,
		},
		RoleFinance: {
			CapabilityDocumentsProcess: true, CapabilityClaimsReview: true,
			CapabilityReviewSourceRead: true, CapabilityFactsRead: true,
			CapabilityAllocationsManage: true,
		},
		RoleReviewer: {
			CapabilityDocumentsProcess: true, CapabilityClaimsReview: true,
			CapabilityReviewSourceRead: true,
		},
		RoleViewer: {CapabilityFactsRead: true},
	}
	for _, role := range []Role{RoleOwner, RoleFinance, RoleReviewer, RoleViewer} {
		role := role
		for _, capability := range capabilities {
			capability := capability
			t.Run(string(role)+"/"+string(capability), func(t *testing.T) {
				t.Parallel()
				if got, want := role.Allows(capability), allowed[role][capability]; got != want {
					t.Fatalf("Allows() = %v, want %v", got, want)
				}
				context := TenantContext{TenantID: "tenant", UserID: "user", Role: role}
				if err := context.Require(capability); (err == nil) != allowed[role][capability] {
					t.Fatalf("Require() error = %v", err)
				}
			})
		}
		if got := role.Capabilities(); len(got) != len(allowed[role]) {
			t.Fatalf("%s capabilities length = %d, want %d", role, len(got), len(allowed[role]))
		}
		for _, capability := range role.Capabilities() {
			if !allowed[role][capability] {
				t.Fatalf("%s returned unexpected capability %s", role, capability)
			}
		}
	}
}

func TestTenantContextRequire(t *testing.T) {
	t.Parallel()

	if err := (TenantContext{}).Require(CapabilityFactsRead); err != ErrUnauthenticated {
		t.Fatalf("empty context error = %v", err)
	}
	context := TenantContext{TenantID: "tenant", UserID: "user", Role: RoleViewer}
	if err := context.Require(CapabilityClaimsReview); err != ErrForbidden {
		t.Fatalf("forbidden capability error = %v", err)
	}
	if err := context.Require(CapabilityFactsRead); err != nil {
		t.Fatalf("allowed capability error = %v", err)
	}
}
