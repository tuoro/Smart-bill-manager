package domain

import "slices"

type Role string

// 只有两档：管理员什么都能做；成员做全部日常业务，碰不了系统设置、不能删除。
// 这个产品是几个人一起记账报销，人人都要上传、审核、整理，更细的分工只会让邀请
// 时多想一步、邀错档位。
const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
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
	CapabilityInsightsRead          Capability = "insights.read"
	CapabilityResourcesDelete       Capability = "resources.delete"
	// CapabilityDeploymentManage 覆盖数据库连接等部署级设置，只授予 Owner。
	CapabilityDeploymentManage Capability = "deployment.manage"
)

var roleCapabilities = map[Role][]Capability{
	RoleOwner: {
		CapabilityDeploymentManage,
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
		CapabilityInsightsRead,
		CapabilityResourcesDelete,
	},
	RoleMember: {
		CapabilityDocumentsProcess,
		CapabilityClaimsReview,
		CapabilityReviewSourceRead,
		CapabilityFactsRead,
		CapabilityAllocationsManage,
		CapabilityTripAssignmentsManage,
		// 邮箱是每个成员自己的，所以成员可以登记和删除自己的邮箱来源。
		CapabilityEmailSourcesManage,
		CapabilityEmailArchiveRead,
		CapabilityReimbursementsRead,
		CapabilityReimbursementsManage,
		CapabilityInsightsRead,
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
