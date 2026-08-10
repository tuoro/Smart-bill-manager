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
	updates, err := mergeEmailLogFields(&best, []models.EmailLog{
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
	if err != nil {
		t.Fatalf("合并邮件日志字段失败: %v", err)
	}

	if _, exists := updates["subject"]; exists || best.Subject == nil || *best.Subject != "原主题" {
		t.Fatal("合并不得覆盖保留记录的已有主题")
	}
	if best.FromAddress == nil || *best.FromAddress != "sender@example.com" {
		t.Fatalf("缺失发件人未合并: %v", best.FromAddress)
	}
	if best.ParsedInvoiceID == nil || *best.ParsedInvoiceID != "invoice-1" {
		t.Fatalf("单个解析结果未规范到主发票字段: %v", best.ParsedInvoiceID)
	}
	if best.ParsedInvoiceIDs != nil {
		t.Fatalf("单个解析结果不应保留列表字段: %v", best.ParsedInvoiceIDs)
	}
	if best.HasAttachment != 1 || best.AttachmentCount != 3 {
		t.Fatalf("附件元数据未取最大值: has=%d count=%d", best.HasAttachment, best.AttachmentCount)
	}
	if best.Status != "parsed" {
		t.Fatalf("已有状态不应被覆盖: %s", best.Status)
	}
}

func TestMergeEmailLogFieldsPreservesAllInvoiceAssociationsDeterministically(t *testing.T) {
	best := models.EmailLog{
		ID:               "best",
		ParsedInvoiceID:  unitStringPointer(" invoice-2 "),
		ParsedInvoiceIDs: unitStringPointer(`["invoice-2"," invoice-3 ","invoice-3"]`),
	}
	updates, err := mergeEmailLogFields(&best, []models.EmailLog{
		best,
		{
			ID:               "other",
			ParsedInvoiceID:  unitStringPointer("invoice-1"),
			ParsedInvoiceIDs: unitStringPointer(`[" invoice-4 ","invoice-3","invoice-1"]`),
		},
	})
	if err != nil {
		t.Fatalf("合并不同发票关联失败: %v", err)
	}
	if best.ParsedInvoiceID == nil || *best.ParsedInvoiceID != "invoice-2" {
		t.Fatalf("未按保留记录的 scalar 确定主发票: %v", best.ParsedInvoiceID)
	}
	if best.ParsedInvoiceIDs == nil || *best.ParsedInvoiceIDs != `["invoice-2","invoice-1","invoice-3","invoice-4"]` {
		t.Fatalf("未规范保留全部发票关联: %v", best.ParsedInvoiceIDs)
	}
	if got := updates["parsed_invoice_ids"]; got != `["invoice-2","invoice-1","invoice-3","invoice-4"]` {
		t.Fatalf("发票列表更新值错误: %v", got)
	}
}

func TestMergeEmailLogFieldsRejectsCorruptInvoiceListWithoutMutation(t *testing.T) {
	best := models.EmailLog{
		ID:              "best",
		ParsedInvoiceID: unitStringPointer(" invoice-2 "),
	}
	_, err := mergeEmailLogFields(&best, []models.EmailLog{
		best,
		{ID: "corrupt", ParsedInvoiceIDs: unitStringPointer(`["invoice-1"`)},
	})
	if err == nil {
		t.Fatal("损坏的 parsed_invoice_ids 必须拒绝合并")
	}
	if best.ParsedInvoiceID == nil || *best.ParsedInvoiceID != " invoice-2 " {
		t.Fatalf("校验失败前不得修改保留记录: %v", best.ParsedInvoiceID)
	}
	if best.ParsedInvoiceIDs != nil {
		t.Fatalf("校验失败前不得写入列表字段: %v", best.ParsedInvoiceIDs)
	}
}

func unitStringPointer(value string) *string {
	return &value
}
