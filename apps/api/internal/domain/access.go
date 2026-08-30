package domain

import "slices"

type Role string

const (
	RoleOwner    Role = "owner"
	RoleFinance  Role = "finance"
	RoleReviewer Role = "reviewer"
	RoleViewer   Role = "viewer"
)

type Capability string

const (
	CapabilityMembersManage         Capability = "members.manage"
	CapabilityProvidersManage       Capability = "providers.manage"
	CapabilityDocumentsProcess      Capability = "documents.process"
	CapabilityClaimsReview          Capability = "claims.review"
	CapabilityReviewSourceRead      Capability = "review.source.read"
	CapabilityFactsRead             Capability = "facts.read"
	CapabilityAllocationsManage     Capability = "allocations.manage"
	CapabilityTripAssignmentsManage Capability = "trip_assignments.manage"
	CapabilityEmailSourcesManage    Capability = "email_sources.manage"
	CapabilityEmailArchiveRead      Capability = "email_archive.read"
	CapabilityReimbursementsRead    Capability = "reimbursements.read"
	CapabilityReimbursementsManage  Capability = "reimbursements.manage"
	CapabilityResourcesDelete       Capability = "resources.delete"
)

var roleCapabilities = map[Role][]Capability{
	RoleOwner: {
		CapabilityMembersManage,
		CapabilityProvidersManage,
		CapabilityDocumentsProcess,
		CapabilityClaimsReview,
		CapabilityReviewSourceRead,
		CapabilityFactsRead,
		CapabilityAllocationsManage,
		CapabilityTripAssignmentsManage,
		CapabilityEmailSourcesManage,
		CapabilityEmailArchiveRead,
		CapabilityReimbursementsRead,
		CapabilityReimbursementsManage,
		CapabilityResourcesDelete,
	},
	RoleFinance: {
		CapabilityDocumentsProcess,
		CapabilityClaimsReview,
		CapabilityReviewSourceRead,
		CapabilityFactsRead,
		CapabilityAllocationsManage,
		CapabilityTripAssignmentsManage,
		CapabilityEmailArchiveRead,
		CapabilityReimbursementsRead,
		CapabilityReimbursementsManage,
	},
	RoleReviewer: {
		CapabilityDocumentsProcess,
		CapabilityClaimsReview,
		CapabilityReviewSourceRead,
	},
	RoleViewer: {
		CapabilityFactsRead,
		CapabilityReimbursementsRead,
	},
}

type TenantContext struct {
	TenantID string
	UserID   string
	Role     Role
}

func (r Role) Valid() bool {
	_, ok := roleCapabilities[r]
	return ok
}

func (r Role) Capabilities() []Capability {
	return slices.Clone(roleCapabilities[r])
}

func (r Role) Allows(capability Capability) bool {
	return slices.Contains(roleCapabilities[r], capability)
}

func (c TenantContext) Require(capability Capability) error {
	if c.TenantID == "" || c.UserID == "" || !c.Role.Valid() {
		return ErrUnauthenticated
	}
	if !c.Role.Allows(capability) {
		return ErrForbidden
	}
	return nil
}
