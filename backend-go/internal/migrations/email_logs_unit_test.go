package migrations

import (
	"testing"
	"time"

	"smart-bill-manager/internal/models"
)

func TestSelectBestEmailLogUsesQualityThenStableTieBreak(t *testing.T) {
	createdAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	best := selectBestEmailLog([]models.EmailLog{
		{ID: "received", Status: "received", CreatedAt: createdAt.Add(time.Hour)},
		{ID: "parsed", Status: "parsed", ParsedInvoiceID: unitStringPointer("invoice-1"), CreatedAt: createdAt},
	})
	if best.ID != "parsed" {
		t.Fatalf("应保留解析信息更完整的邮件日志，实际保留 %s", best.ID)
	}

	best = selectBestEmailLog([]models.EmailLog{
		{ID: "log-b", Status: "received", CreatedAt: createdAt},
		{ID: "log-a", Status: "received", CreatedAt: createdAt},
	})
	if best.ID != "log-a" {
		t.Fatalf("质量和时间相同时应按 ID 稳定选择，实际保留 %s", best.ID)
	}
}

func TestMergeEmailLogFieldsFillsMissingValuesOnly(t *testing.T) {
	best := models.EmailLog{
		ID:              "best",
		Subject:         unitStringPointer("原主题"),
		AttachmentCount: 1,
		Status:          "parsed",
	}
	updates := mergeEmailLogFields(&best, []models.EmailLog{
		best,
		{
			ID:               "other",
			Subject:          unitStringPointer("不得覆盖"),
			FromAddress:      unitStringPointer(" sender@example.com "),
			HasAttachment:    1,
			AttachmentCount:  3,
			ParsedInvoiceIDs: unitStringPointer(`["invoice-1"]`),
			Status:           "error",
		},
	})

	if _, exists := updates["subject"]; exists || best.Subject == nil || *best.Subject != "原主题" {
		t.Fatal("合并不得覆盖保留记录的已有主题")
	}
	if best.FromAddress == nil || *best.FromAddress != "sender@example.com" {
		t.Fatalf("缺失发件人未合并: %v", best.FromAddress)
	}
	if best.ParsedInvoiceIDs == nil || *best.ParsedInvoiceIDs != `["invoice-1"]` {
		t.Fatalf("缺失解析结果列表未合并: %v", best.ParsedInvoiceIDs)
	}
	if best.HasAttachment != 1 || best.AttachmentCount != 3 {
		t.Fatalf("附件元数据未取最大值: has=%d count=%d", best.HasAttachment, best.AttachmentCount)
	}
	if best.Status != "parsed" {
		t.Fatalf("已有状态不应被覆盖: %s", best.Status)
	}
}

func unitStringPointer(value string) *string {
	return &value
}
