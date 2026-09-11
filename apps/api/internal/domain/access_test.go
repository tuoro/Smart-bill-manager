package domain

import "testing"

func TestRoleCapabilityMatrix(t *testing.T) {
	t.Parallel()

	capabilities := []Capability{
		CapabilityDeploymentManage,
		CapabilityMembersManage,
		CapabilityProvidersManage,
		CapabilityDocumentsProcess,
		CapabilityClaimsReview,
		CapabilityReviewSourceRead,
		CapabilityFactsRead,
		CapabilityAllocationsManage,
		CapabilityEmailSourcesManage,
		CapabilityEmailArchiveRead,
		CapabilityTripAssignmentsManage,
		CapabilityReimbursementsRead,
		CapabilityReimbursementsManage,
		CapabilityInsightsRead,
		CapabilityResourcesDelete,
	}
	allowed := map[Role]map[Capability]bool{
		RoleOwner: {
			// 部署级设置（数据库连接）只有 Owner 可改。
			CapabilityDeploymentManage: true,
			CapabilityMembersManage:    true, CapabilityProvidersManage: true,
			CapabilityDocumentsProcess: true, CapabilityClaimsReview: true,
			CapabilityReviewSourceRead: true, CapabilityFactsRead: true,
			CapabilityAllocationsManage:     true,
			CapabilityEmailSourcesManage:    true,
			CapabilityEmailArchiveRead:      true,
			CapabilityTripAssignmentsManage: true,
			CapabilityReimbursementsRead:    true,
			CapabilityReimbursementsManage:  true,
			CapabilityInsightsRead:          true,
			CapabilityResourcesDelete:       true,
		},
		RoleMember: {
			CapabilityDocumentsProcess: true, CapabilityClaimsReview: true,
			CapabilityReviewSourceRead: true, CapabilityFactsRead: true,
			CapabilityAllocationsManage:     true,
			CapabilityEmailSourcesManage:    true,
			CapabilityEmailArchiveRead:      true,
			CapabilityTripAssignmentsManage: true,
			CapabilityReimbursementsRead:    true,
			CapabilityReimbursementsManage:  true,
			CapabilityInsightsRead:          true,
		},
	}
	for _, role := range []Role{RoleOwner, RoleMember} {
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
	context := TenantContext{TenantID: "tenant", UserID: "user", Role: RoleMember}
	if err := context.Require(CapabilityMembersManage); err != ErrForbidden {
		t.Fatalf("forbidden capability error = %v", err)
	}
	if unknown := (TenantContext{TenantID: "tenant", UserID: "user", Role: Role("viewer")}); unknown.Require(CapabilityFactsRead) != ErrUnauthenticated {
		t.Fatal("retired role must not be accepted")
	}
	if err := context.Require(CapabilityFactsRead); err != nil {
		t.Fatalf("allowed capability error = %v", err)
	}
}
