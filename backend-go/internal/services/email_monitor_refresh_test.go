package services

import (
	"testing"

	"smart-bill-manager/internal/models"
)

func TestComputeEmailLogMetadataUpdates_ForceRefreshOverridesCounts(t *testing.T) {
	existing := &models.EmailLog{
		HasAttachment:   1,
		AttachmentCount: 9,
	}

	updates := computeEmailLogMetadataUpdates(existing, 0, 2, nil, nil, true)
	if v, ok := updates["has_attachment"]; !ok || v.(int) != 0 {
		t.Fatalf("expected has_attachment override to 0, got %+v", updates)
	}
	if v, ok := updates["attachment_count"]; !ok || v.(int) != 2 {
		t.Fatalf("expected attachment_count override to 2, got %+v", updates)
	}
}

func TestComputeEmailLogMetadataUpdates_NonForceOnlyIncreases(t *testing.T) {
	existing := &models.EmailLog{
		HasAttachment:   1,
		AttachmentCount: 3,
	}

	updates := computeEmailLogMetadataUpdates(existing, 0, 2, nil, nil, false)
	if len(updates) != 0 {
		t.Fatalf("expected no updates when non-force decreases, got %+v", updates)
	}

	updates = computeEmailLogMetadataUpdates(existing, 1, 4, nil, nil, false)
	if v, ok := updates["attachment_count"]; !ok || v.(int) != 4 {
		t.Fatalf("expected attachment_count increase to 4, got %+v", updates)
	}
}

