//go:build cgo

package migrations

import (
	"strings"
	"testing"
	"time"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

func TestRunMigratesEmailLogIdentityIdempotently(t *testing.T) {
	db := openTestDB(t)
	if err := migrateSchema(db); err != nil {
		t.Fatalf("初始化旧版业务结构失败: %v", err)
	}

	createdAt := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	rows := []models.EmailLog{
		{
			ID:               "log-parsed",
			OwnerUserID:      "owner-1",
			EmailConfigID:    "config-1",
			Mailbox:          "INBOX",
			MessageUID:       42,
			AttachmentCount:  1,
			ParsedInvoiceID:  stringPointer(" invoice-2 "),
			ParsedInvoiceIDs: stringPointer(`["invoice-2"," invoice-3 ","invoice-3"]`),
			Status:           "parsed",
			CreatedAt:        createdAt.Add(2 * time.Hour),
		},
		{
			ID:               "log-parsed-other",
			OwnerUserID:      "owner-1",
			EmailConfigID:    "config-1",
			Mailbox:          "INBOX",
			MessageUID:       42,
			ParsedInvoiceID:  stringPointer("invoice-1"),
			ParsedInvoiceIDs: stringPointer(`["invoice-1","invoice-3"," invoice-4 "]`),
			Status:           "parsed",
			CreatedAt:        createdAt.Add(time.Hour),
		},
		{
			ID:              "log-metadata",
			OwnerUserID:     "owner-1",
			EmailConfigID:   "config-1",
			Mailbox:         "INBOX",
			MessageUID:      42,
			Subject:         stringPointer("  发票邮件  "),
			FromAddress:     stringPointer("sender@example.com"),
			ReceivedDate:    stringPointer("2026-08-01T09:00:00Z"),
			HasAttachment:   1,
			AttachmentCount: 4,
			InvoiceXMLURL:   stringPointer("https://example.com/invoice.xml"),
			InvoicePDFURL:   stringPointer("https://example.com/invoice.pdf"),
			ParseError:      stringPointer("历史解析告警"),
			Status:          "received",
			CreatedAt:       createdAt.Add(3 * time.Hour),
		},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("写入重复邮件日志失败: %v", err)
	}

	if err := Run(db); err != nil {
		t.Fatalf("执行邮件日志身份迁移失败: %v", err)
	}
	assertEmailLogGroupCount(t, db, 1)

	var got models.EmailLog
	if err := db.First(&got, "id = ?", "log-parsed").Error; err != nil {
		t.Fatalf("读取合并后的邮件日志失败: %v", err)
	}
	if got.Status != "parsed" || got.ParsedInvoiceID == nil || *got.ParsedInvoiceID != "invoice-2" {
		t.Fatalf("迁移未保留质量最高的解析记录: %#v", got)
	}
	assertEmailLogText(t, "subject", got.Subject, "发票邮件")
	assertEmailLogText(t, "from_address", got.FromAddress, "sender@example.com")
	assertEmailLogText(t, "received_date", got.ReceivedDate, "2026-08-01T09:00:00Z")
	assertEmailLogText(t, "invoice_xml_url", got.InvoiceXMLURL, "https://example.com/invoice.xml")
	assertEmailLogText(t, "invoice_pdf_url", got.InvoicePDFURL, "https://example.com/invoice.pdf")
	assertEmailLogText(t, "parsed_invoice_ids", got.ParsedInvoiceIDs, `["invoice-2","invoice-1","invoice-3","invoice-4"]`)
	assertEmailLogText(t, "parse_error", got.ParseError, "历史解析告警")
	if got.HasAttachment != 1 || got.AttachmentCount != 4 {
		t.Fatalf("附件元数据未合并: has=%d count=%d", got.HasAttachment, got.AttachmentCount)
	}

	var indexCount int64
	if err := db.Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?",
		emailLogIdentityIndex,
	).Scan(&indexCount).Error; err != nil {
		t.Fatalf("检查邮件日志身份索引失败: %v", err)
	}
	if indexCount != 1 {
		t.Fatalf("邮件日志身份索引应创建一次，实际为 %d", indexCount)
	}
	duplicate := models.EmailLog{
		ID:            "log-new-duplicate",
		OwnerUserID:   "owner-1",
		EmailConfigID: "config-1",
		Mailbox:       "INBOX",
		MessageUID:    42,
		Status:        "received",
	}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("唯一索引应拒绝再次写入相同邮件身份")
	}

	if err := Run(db); err != nil {
		t.Fatalf("重复执行迁移失败: %v", err)
	}
	assertEmailLogGroupCount(t, db, 1)
	var rerun models.EmailLog
	if err := db.First(&rerun, "id = ?", "log-parsed").Error; err != nil {
		t.Fatalf("读取重跑后的邮件日志失败: %v", err)
	}
	assertEmailLogText(t, "parsed_invoice_id", rerun.ParsedInvoiceID, "invoice-2")
	assertEmailLogText(t, "parsed_invoice_ids", rerun.ParsedInvoiceIDs, `["invoice-2","invoice-1","invoice-3","invoice-4"]`)
	var migrationCount int64
	if err := db.Model(&schemaMigration{}).
		Where("version = ?", emailLogIdentityMigrationVersion).
		Count(&migrationCount).Error; err != nil {
		t.Fatalf("统计邮件日志身份迁移记录失败: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("邮件日志身份迁移记录应只有一条，实际为 %d", migrationCount)
	}
}

func TestRunRollsBackEmailLogDedupeWhenParsedInvoiceIDsAreCorrupt(t *testing.T) {
	db := openTestDB(t)
	if err := migrateSchema(db); err != nil {
		t.Fatalf("初始化旧版业务结构失败: %v", err)
	}

	createdAt := time.Date(2026, 8, 3, 8, 0, 0, 0, time.UTC)
	rows := []models.EmailLog{
		{
			ID:              "log-a-best",
			OwnerUserID:     "owner-a",
			EmailConfigID:   "config-a",
			Mailbox:         "INBOX",
			MessageUID:      1,
			ParsedInvoiceID: stringPointer("invoice-a"),
			Status:          "parsed",
			CreatedAt:       createdAt,
		},
		{
			ID:            "log-a-other",
			OwnerUserID:   "owner-a",
			EmailConfigID: "config-a",
			Mailbox:       "INBOX",
			MessageUID:    1,
			Subject:       stringPointer("重跑后才允许合并"),
			Status:        "received",
			CreatedAt:     createdAt.Add(time.Hour),
		},
		{
			ID:              "log-b-best",
			OwnerUserID:     "owner-b",
			EmailConfigID:   "config-b",
			Mailbox:         "INBOX",
			MessageUID:      2,
			ParsedInvoiceID: stringPointer("invoice-b1"),
			Status:          "parsed",
			CreatedAt:       createdAt,
		},
		{
			ID:               "log-b-corrupt",
			OwnerUserID:      "owner-b",
			EmailConfigID:    "config-b",
			Mailbox:          "INBOX",
			MessageUID:       2,
			ParsedInvoiceIDs: stringPointer(`["invoice-b2"`),
			Status:           "parsed",
			CreatedAt:        createdAt.Add(time.Hour),
		},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("写入重复邮件日志失败: %v", err)
	}

	err := Run(db)
	if err == nil || !strings.Contains(err.Error(), "parsed_invoice_ids") {
		t.Fatalf("损坏的 parsed_invoice_ids 应阻止迁移，实际错误为 %v", err)
	}
	assertEmailLogGroupCountFor(t, db, "owner-a", "config-a", "INBOX", 1, 2)
	assertEmailLogGroupCountFor(t, db, "owner-b", "config-b", "INBOX", 2, 2)

	var firstGroupBest models.EmailLog
	if err := db.First(&firstGroupBest, "id = ?", "log-a-best").Error; err != nil {
		t.Fatalf("读取回滚后的首组邮件日志失败: %v", err)
	}
	if firstGroupBest.Subject != nil {
		t.Fatalf("后续组校验失败时，先前组的更新必须回滚: %q", *firstGroupBest.Subject)
	}
	var migrationCount int64
	if err := db.Model(&schemaMigration{}).
		Where("version = ?", emailLogIdentityMigrationVersion).
		Count(&migrationCount).Error; err != nil {
		t.Fatalf("统计失败迁移记录失败: %v", err)
	}
	if migrationCount != 0 {
		t.Fatalf("损坏数据导致的失败迁移不得写入版本记录，实际为 %d", migrationCount)
	}

	validList := `["invoice-b1","invoice-b2"]`
	if err := db.Model(&models.EmailLog{}).
		Where("id = ?", "log-b-corrupt").
		Update("parsed_invoice_ids", validList).Error; err != nil {
		t.Fatalf("修复损坏的测试数据失败: %v", err)
	}
	if err := Run(db); err != nil {
		t.Fatalf("修复数据后重跑迁移失败: %v", err)
	}
	assertEmailLogGroupCountFor(t, db, "owner-a", "config-a", "INBOX", 1, 1)
	assertEmailLogGroupCountFor(t, db, "owner-b", "config-b", "INBOX", 2, 1)
}

func TestRunRollsBackEmailLogDedupeWhenIdentityIndexIsInvalid(t *testing.T) {
	db := openTestDB(t)
	if err := migrateSchema(db); err != nil {
		t.Fatalf("初始化旧版业务结构失败: %v", err)
	}
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		t.Fatalf("初始化迁移记录表失败: %v", err)
	}
	if err := db.Exec(`
		CREATE INDEX idx_email_logs_owner_cfg_box_uid
		ON email_logs(created_at)
	`).Error; err != nil {
		t.Fatalf("创建冲突索引失败: %v", err)
	}

	createdAt := time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
	rows := []models.EmailLog{
		{
			ID:              "log-best",
			OwnerUserID:     "owner-2",
			EmailConfigID:   "config-2",
			Mailbox:         "INBOX",
			MessageUID:      7,
			ParsedInvoiceID: stringPointer("invoice-2"),
			Status:          "parsed",
			CreatedAt:       createdAt,
		},
		{
			ID:            "log-other",
			OwnerUserID:   "owner-2",
			EmailConfigID: "config-2",
			Mailbox:       "INBOX",
			MessageUID:    7,
			Subject:       stringPointer("不得留下的半迁移字段"),
			Status:        "received",
			CreatedAt:     createdAt.Add(time.Hour),
		},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("写入重复邮件日志失败: %v", err)
	}

	err := Run(db)
	if err == nil || !strings.Contains(err.Error(), "邮件日志身份索引") {
		t.Fatalf("无效身份索引应阻止迁移，实际错误为 %v", err)
	}
	assertEmailLogGroupCountFor(t, db, "owner-2", "config-2", "INBOX", 7, 2)

	var best models.EmailLog
	if err := db.First(&best, "id = ?", "log-best").Error; err != nil {
		t.Fatalf("读取回滚后的邮件日志失败: %v", err)
	}
	if best.Subject != nil {
		t.Fatalf("迁移失败后合并更新未回滚: subject=%q", *best.Subject)
	}
	var migrationCount int64
	if err := db.Model(&schemaMigration{}).
		Where("version = ?", emailLogIdentityMigrationVersion).
		Count(&migrationCount).Error; err != nil {
		t.Fatalf("统计失败迁移记录失败: %v", err)
	}
	if migrationCount != 0 {
		t.Fatalf("失败迁移不得写入版本记录，实际为 %d", migrationCount)
	}
}

func assertEmailLogGroupCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	assertEmailLogGroupCountFor(t, db, "owner-1", "config-1", "INBOX", 42, want)
}

func assertEmailLogGroupCountFor(
	t *testing.T,
	db *gorm.DB,
	ownerUserID string,
	emailConfigID string,
	mailbox string,
	messageUID uint32,
	want int64,
) {
	t.Helper()
	var count int64
	if err := db.Model(&models.EmailLog{}).
		Where(
			"owner_user_id = ? AND email_config_id = ? AND mailbox = ? AND message_uid = ?",
			ownerUserID,
			emailConfigID,
			mailbox,
			messageUID,
		).
		Count(&count).Error; err != nil {
		t.Fatalf("统计邮件日志身份组失败: %v", err)
	}
	if count != want {
		t.Fatalf("邮件日志身份组应有 %d 条，实际为 %d", want, count)
	}
}

func assertEmailLogText(t *testing.T, field string, got *string, want string) {
	t.Helper()
	if got == nil || *got != want {
		t.Fatalf("邮件日志字段 %s 迁移错误: got=%v want=%q", field, got, want)
	}
}
