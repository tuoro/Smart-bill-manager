//go:build cgo

package migrations

import (
	"strings"
	"testing"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

func TestRemoveRepoRegressionSamplesIsExactAndIdempotent(t *testing.T) {
	db := openTestDB(t)
	if err := migrateSchema(db); err != nil {
		t.Fatalf("初始化业务结构失败: %v", err)
	}
	createLegacyRegressionSamplesTable(t, db)

	rows := []struct {
		id, origin, sourceType, createdBy string
	}{
		{id: "auto-repo-row", origin: legacyRepoRegressionOrigin, sourceType: legacyRepoRegressionSource, createdBy: legacyRepoRegressionCreatedBy},
		{id: "manual-author-row", origin: legacyRepoRegressionOrigin, sourceType: legacyRepoRegressionSource, createdBy: "synthetic-user"},
		{id: "manual-origin-row", origin: "ui", sourceType: legacyRepoRegressionSource, createdBy: legacyRepoRegressionCreatedBy},
		{id: "manual-source-row", origin: legacyRepoRegressionOrigin, sourceType: "invoice", createdBy: legacyRepoRegressionCreatedBy},
		{id: "manual-row", origin: "ui", sourceType: "invoice", createdBy: "synthetic-user"},
	}
	for _, row := range rows {
		insertLegacyRegressionSample(t, db, row.id, row.origin, row.sourceType, row.createdBy)
	}
	insertProductionSentinels(t, db)

	if err := Run(db); err != nil {
		t.Fatalf("执行生产迁移失败: %v", err)
	}
	if err := Run(db); err != nil {
		t.Fatalf("重复执行生产迁移失败: %v", err)
	}
	if err := removeRepoRegressionSamples(db); err != nil {
		t.Fatalf("重复直接执行清理迁移失败: %v", err)
	}

	assertLegacyRegressionRowCount(t, db, "auto-repo-row", 0)
	for _, id := range []string{"manual-author-row", "manual-origin-row", "manual-source-row", "manual-row"} {
		assertLegacyRegressionRowCount(t, db, id, 1)
	}
	assertProductionSentinels(t, db)
}

func TestRemoveRepoRegressionSamplesFailureIsRecordedAsMigrationFailure(t *testing.T) {
	db := openTestDB(t)
	if err := migrateSchema(db); err != nil {
		t.Fatalf("初始化业务结构失败: %v", err)
	}
	createLegacyRegressionSamplesTable(t, db)
	insertLegacyRegressionSample(t, db, "auto-repo-row", legacyRepoRegressionOrigin, legacyRepoRegressionSource, legacyRepoRegressionCreatedBy)
	if err := db.Exec(`
		CREATE TRIGGER block_regression_cleanup
		BEFORE DELETE ON regression_samples
		BEGIN
			SELECT RAISE(ABORT, 'synthetic cleanup failure');
		END
	`).Error; err != nil {
		t.Fatalf("创建失败注入触发器失败: %v", err)
	}

	err := Run(db)
	if err == nil || !strings.Contains(err.Error(), "remove_repo_regression_samples") {
		t.Fatalf("应以清理迁移失败阻止启动，实际错误为 %v", err)
	}
	assertLegacyRegressionRowCount(t, db, "auto-repo-row", 1)

	var applied int64
	if err := db.Model(&schemaMigration{}).
		Where("version = ?", removeRepoRegressionSamplesMigrationVersion).
		Count(&applied).Error; err != nil {
		t.Fatalf("读取迁移记录失败: %v", err)
	}
	if applied != 0 {
		t.Fatal("失败的清理迁移不应记录为已应用")
	}
}

func createLegacyRegressionSamplesTable(t *testing.T, db interface{ Exec(string, ...any) *gorm.DB }) {
	t.Helper()
	if err := db.Exec(`
		CREATE TABLE regression_samples (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			name TEXT NOT NULL,
			origin TEXT NOT NULL,
			source_type TEXT NOT NULL,
			source_id TEXT NOT NULL,
			created_by TEXT NOT NULL,
			raw_text TEXT NOT NULL,
			raw_hash TEXT NOT NULL DEFAULT '',
			expected_json TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("创建旧回归样本表失败: %v", err)
	}
}

func insertLegacyRegressionSample(t *testing.T, db interface{ Exec(string, ...any) *gorm.DB }, id, origin, sourceType, createdBy string) {
	t.Helper()
	if err := db.Exec(`
		INSERT INTO regression_samples (
			id, kind, name, origin, source_type, source_id, created_by,
			raw_text, raw_hash, expected_json
		) VALUES (?, 'invoice', 'synthetic-row', ?, ?, 'synthetic-source', ?, 'SYNTHETIC', 'synthetic-hash', '{}')
	`, id, origin, sourceType, createdBy).Error; err != nil {
		t.Fatalf("写入旧回归样本失败: %v", err)
	}
}

func assertLegacyRegressionRowCount(t *testing.T, db interface{ Raw(string, ...any) *gorm.DB }, id string, want int64) {
	t.Helper()
	var got int64
	if err := db.Raw("SELECT COUNT(*) FROM regression_samples WHERE id = ?", id).Scan(&got).Error; err != nil {
		t.Fatalf("读取旧回归样本失败: %v", err)
	}
	if got != want {
		t.Fatalf("旧回归样本 %q 数量为 %d，预期 %d", id, got, want)
	}
}

func insertProductionSentinels(t *testing.T, db *gorm.DB) {
	t.Helper()
	extracted := `{"synthetic":true}`
	raw := "SYNTHETIC"
	if err := db.Create(&models.Payment{
		ID: "production-payment", OwnerUserID: "synthetic-owner", Amount: 1,
		TransactionTime: "2026-08-01 00:00:00", ExtractedData: &extracted,
	}).Error; err != nil {
		t.Fatalf("写入正式支付哨兵失败: %v", err)
	}
	if err := db.Create(&models.Invoice{
		ID: "production-invoice", OwnerUserID: "synthetic-owner", Filename: "synthetic.pdf",
		OriginalName: "synthetic.pdf", FilePath: "uploads/synthetic.pdf", RawText: &raw,
	}).Error; err != nil {
		t.Fatalf("写入正式发票哨兵失败: %v", err)
	}
	if err := db.Create(&models.PaymentOCRBlob{
		PaymentID: "production-payment", OwnerUserID: "synthetic-owner", ExtractedData: &extracted,
	}).Error; err != nil {
		t.Fatalf("写入正式支付 OCR 哨兵失败: %v", err)
	}
	if err := db.Create(&models.InvoiceOCRBlob{
		InvoiceID: "production-invoice", OwnerUserID: "synthetic-owner", ExtractedData: &extracted, RawText: &raw,
	}).Error; err != nil {
		t.Fatalf("写入正式发票 OCR 哨兵失败: %v", err)
	}
	if err := db.Create(&models.EmailConfig{
		ID: "production-email", OwnerUserID: "synthetic-owner", Email: "synthetic@example.invalid",
		IMAPHost: "imap.example.invalid", Password: "SYNTHETIC",
	}).Error; err != nil {
		t.Fatalf("写入正式邮箱配置哨兵失败: %v", err)
	}
	if err := db.Create(&models.EmailLog{
		ID: "production-email-log", OwnerUserID: "synthetic-owner", EmailConfigID: "production-email",
		Mailbox: "INBOX", MessageUID: 1,
	}).Error; err != nil {
		t.Fatalf("写入正式邮件日志哨兵失败: %v", err)
	}
}

func assertProductionSentinels(t *testing.T, db *gorm.DB) {
	t.Helper()
	checks := []struct {
		model  any
		column string
		id     string
	}{
		{model: &models.Payment{}, column: "id", id: "production-payment"},
		{model: &models.Invoice{}, column: "id", id: "production-invoice"},
		{model: &models.PaymentOCRBlob{}, column: "payment_id", id: "production-payment"},
		{model: &models.InvoiceOCRBlob{}, column: "invoice_id", id: "production-invoice"},
		{model: &models.EmailConfig{}, column: "id", id: "production-email"},
		{model: &models.EmailLog{}, column: "id", id: "production-email-log"},
	}
	for _, check := range checks {
		var count int64
		if err := db.Model(check.model).Where(check.column+" = ?", check.id).Count(&count).Error; err != nil {
			t.Fatalf("读取正式数据哨兵失败: %v", err)
		}
		if count != 1 {
			t.Fatalf("正式数据哨兵 %q 被清理迁移修改", check.id)
		}
	}
}
