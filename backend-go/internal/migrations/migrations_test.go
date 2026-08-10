//go:build cgo

package migrations

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"smart-bill-manager/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunMigratesLegacyDataIdempotently(t *testing.T) {
	db := openTestDB(t)
	if err := migrateSchema(db); err != nil {
		t.Fatalf("初始化旧结构失败: %v", err)
	}
	if err := db.Exec("ALTER TABLE payments ADD COLUMN trip_assign_src TEXT").Error; err != nil {
		t.Fatalf("添加旧来源列失败: %v", err)
	}
	if err := db.Exec("ALTER TABLE payments ADD COLUMN trip_assign_state TEXT").Error; err != nil {
		t.Fatalf("添加旧状态列失败: %v", err)
	}

	createdAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Create(&models.User{
		ID: "user-1", Username: "legacy-admin", Password: "hash", Role: "admin", CreatedAt: createdAt,
	}).Error; err != nil {
		t.Fatalf("写入旧用户失败: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO payments (
			id, owner_user_id, is_draft, trip_assignment_source, trip_assignment_state,
			amount, transaction_time, transaction_time_ts, extracted_data, dedup_status,
			created_at, trip_assign_src, trip_assign_state
		) VALUES (?, '', 0, '', '', 12.345, ?, 0, ?, 'ok', ?, 'manual', 'assigned')
	`, "payment-1", "2026-01-02 03:04:05", `{"amount":12.34}`, createdAt).Error; err != nil {
		t.Fatalf("写入旧支付失败: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO trips (
			id, owner_user_id, name, start_time, end_time, start_time_ts, end_time_ts,
			timezone, reimburse_status, bad_debt_locked, created_at, updated_at
		) VALUES (?, '', '旧行程', ?, ?, 0, 0, 'Asia/Shanghai', 'unreimbursed', 0, ?, ?)
	`, "trip-1", "2026-01-03 08:00:00", "2026-01-04 18:00:00", createdAt, createdAt).Error; err != nil {
		t.Fatalf("写入旧行程失败: %v", err)
	}

	insertLegacyInvoice(t, db, "invoice-existing", "2026-01-05", `{"existing":true}`, "", createdAt)
	insertLegacyInvoice(t, db, "invoice-new", "2026年01月06日", `{"new":true}`, "原始文本", createdAt)
	if err := db.Create(&models.InvoiceOCRBlob{
		InvoiceID: "invoice-existing", OwnerUserID: "", ExtractedData: stringPointer(`{"kept":true}`),
	}).Error; err != nil {
		t.Fatalf("写入已有 OCR 大字段失败: %v", err)
	}

	if err := Run(db); err != nil {
		t.Fatalf("首次执行迁移失败: %v", err)
	}
	if err := Run(db); err != nil {
		t.Fatalf("重复执行迁移失败: %v", err)
	}

	var migrationCount int64
	if err := db.Model(&schemaMigration{}).Count(&migrationCount).Error; err != nil {
		t.Fatalf("读取迁移记录失败: %v", err)
	}
	if migrationCount != int64(len(registeredMigrations)) {
		t.Fatalf("迁移记录应有 %d 条，实际为 %d", len(registeredMigrations), migrationCount)
	}

	var payment models.Payment
	if err := db.First(&payment, "id = ?", "payment-1").Error; err != nil {
		t.Fatalf("读取迁移后支付失败: %v", err)
	}
	if payment.OwnerUserID != "user-1" || payment.TransactionTimeTs == 0 {
		t.Fatalf("支付回填不完整: owner=%q ts=%d", payment.OwnerUserID, payment.TransactionTimeTs)
	}
	if payment.AmountCents != 1235 || payment.Amount != 12.35 {
		t.Fatalf("支付金额分回填不完整: amount=%v cents=%d", payment.Amount, payment.AmountCents)
	}
	if payment.TripAssignSrc != "manual" || payment.TripAssignState != "assigned" {
		t.Fatalf("旧行程字段未迁移: source=%q state=%q", payment.TripAssignSrc, payment.TripAssignState)
	}

	var trip models.Trip
	if err := db.First(&trip, "id = ?", "trip-1").Error; err != nil {
		t.Fatalf("读取迁移后行程失败: %v", err)
	}
	if trip.OwnerUserID != "user-1" || trip.StartTimeTs == 0 || trip.EndTimeTs == 0 {
		t.Fatalf("行程回填不完整: owner=%q start=%d end=%d", trip.OwnerUserID, trip.StartTimeTs, trip.EndTimeTs)
	}

	var invoice models.Invoice
	if err := db.First(&invoice, "id = ?", "invoice-new").Error; err != nil {
		t.Fatalf("读取迁移后发票失败: %v", err)
	}
	if invoice.OwnerUserID != "user-1" || invoice.InvoiceDateYMD == nil || *invoice.InvoiceDateYMD != "2026-01-06" {
		t.Fatalf("发票回填不完整: owner=%q date=%v", invoice.OwnerUserID, invoice.InvoiceDateYMD)
	}
	if invoice.AmountCents == nil || *invoice.AmountCents != 1001 || invoice.Amount == nil || *invoice.Amount != 10.01 {
		t.Fatalf("发票金额分回填不完整: amount=%v cents=%v", invoice.Amount, invoice.AmountCents)
	}

	var invoiceBlobCount int64
	if err := db.Model(&models.InvoiceOCRBlob{}).Count(&invoiceBlobCount).Error; err != nil {
		t.Fatalf("统计发票 OCR 大字段失败: %v", err)
	}
	if invoiceBlobCount != 2 {
		t.Fatalf("发票 OCR 大字段应为 2 条，实际为 %d", invoiceBlobCount)
	}
	var existingBlob models.InvoiceOCRBlob
	if err := db.First(&existingBlob, "invoice_id = ?", "invoice-existing").Error; err != nil {
		t.Fatalf("读取原有 OCR 大字段失败: %v", err)
	}
	if existingBlob.ExtractedData == nil || *existingBlob.ExtractedData != `{"kept":true}` {
		t.Fatalf("原有 OCR 大字段被覆盖: %v", existingBlob.ExtractedData)
	}
	if existingBlob.OwnerUserID != "user-1" {
		t.Fatalf("原有 OCR 大字段所有者未回填: %q", existingBlob.OwnerUserID)
	}

	var paymentBlobCount int64
	if err := db.Model(&models.PaymentOCRBlob{}).Count(&paymentBlobCount).Error; err != nil {
		t.Fatalf("统计支付 OCR 大字段失败: %v", err)
	}
	if paymentBlobCount != 1 {
		t.Fatalf("支付 OCR 大字段应为 1 条，实际为 %d", paymentBlobCount)
	}
}

func TestRunRejectsNewerDatabaseVersionBeforeBusinessSchemaSync(t *testing.T) {
	db := openTestDB(t)
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		t.Fatalf("初始化迁移表失败: %v", err)
	}
	newerVersion := registeredMigrations[len(registeredMigrations)-1].version + 1
	if err := db.Create(&schemaMigration{Version: newerVersion, Name: "future", AppliedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatalf("写入未来版本失败: %v", err)
	}

	err := Run(db)
	if err == nil || !strings.Contains(err.Error(), "高于程序支持") {
		t.Fatalf("应拒绝未来数据库版本，实际错误为 %v", err)
	}
	if db.Migrator().HasTable(&models.User{}) || db.Migrator().HasTable(&models.EmailLog{}) {
		t.Fatal("拒绝未来数据库版本前不应创建或同步业务表")
	}
}

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("关闭测试数据库失败: %v", err)
		}
	})
	return db
}

func insertLegacyInvoice(t *testing.T, db *gorm.DB, id, date, extractedData, rawText string, createdAt time.Time) {
	t.Helper()
	if err := db.Exec(`
		INSERT INTO invoices (
			id, owner_user_id, is_draft, filename, original_name, file_path,
			invoice_date, amount, extracted_data, raw_text, parse_status, source,
			dedup_status, created_at
		) VALUES (?, '', 0, ?, ?, ?, ?, 10.01, ?, ?, 'success', 'upload', 'ok', ?)
	`, id, id+".pdf", id+".pdf", "uploads/"+id+".pdf", date, extractedData, rawText, createdAt).Error; err != nil {
		t.Fatalf("写入旧发票 %s 失败: %v", id, err)
	}
}

func stringPointer(value string) *string {
	return &value
}
