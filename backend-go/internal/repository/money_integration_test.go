//go:build cgo

package repository

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"smart-bill-manager/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRepositoryWithDBParticipatesInTransaction(t *testing.T) {
	db := openMoneyTestDB(t)
	if err := db.AutoMigrate(&models.Payment{}, &models.Invoice{}); err != nil {
		t.Fatalf("初始化事务测试表失败: %v", err)
	}

	paymentRepo := NewPaymentRepository(db)
	payment := &models.Payment{
		ID: "payment-tx", OwnerUserID: "owner-1", Amount: 10,
		TransactionTime: "2026-01-02T03:04:05Z", TransactionTimeTs: 1,
		TripAssignSrc: "auto", TripAssignState: "no_match", DedupStatus: "ok",
	}
	if err := paymentRepo.Create(payment); err != nil {
		t.Fatalf("创建事务测试支付记录失败: %v", err)
	}

	rollbackErr := errors.New("rollback")
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := paymentRepo.WithDB(tx).Update(payment.ID, map[string]any{"amount": 99}); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("事务应按预期回滚: %v", err)
	}
	storedPayment, err := paymentRepo.FindByID(payment.ID)
	if err != nil {
		t.Fatalf("读取回滚后的支付记录失败: %v", err)
	}
	if storedPayment.Amount != 10 {
		t.Fatalf("支付仓储更新未随事务回滚: amount=%v", storedPayment.Amount)
	}

	amount := 20.0
	invoiceRepo := NewInvoiceRepository(db)
	invoice := &models.Invoice{
		ID: "invoice-tx", OwnerUserID: "owner-1", Filename: "invoice.pdf",
		OriginalName: "invoice.pdf", FilePath: "uploads/invoice.pdf",
		Amount: &amount, ParseStatus: "success", Source: "upload", DedupStatus: "ok",
	}
	if err := invoiceRepo.Create(invoice); err != nil {
		t.Fatalf("创建事务测试发票失败: %v", err)
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := invoiceRepo.WithDB(tx).UpdateForOwner("owner-1", invoice.ID, map[string]any{"amount": 88}); err != nil {
			return err
		}
		return rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("发票事务应按预期回滚: %v", err)
	}
	storedInvoice, err := invoiceRepo.FindByID(invoice.ID)
	if err != nil {
		t.Fatalf("读取回滚后的发票失败: %v", err)
	}
	if storedInvoice.Amount == nil || *storedInvoice.Amount != 20 {
		t.Fatalf("发票仓储更新未随事务回滚: amount=%v", storedInvoice.Amount)
	}
}

func TestMoneyPersistenceUsesCentsAsCanonicalValue(t *testing.T) {
	db := openMoneyTestDB(t)
	if err := db.AutoMigrate(&models.Payment{}, &models.Invoice{}); err != nil {
		t.Fatalf("初始化金额测试表失败: %v", err)
	}

	paymentRepo := NewPaymentRepository(db)
	payment := &models.Payment{
		ID: "payment-1", OwnerUserID: "owner-1", Amount: 10.235,
		TransactionTime: "2026-01-02T03:04:05Z", TransactionTimeTs: 1,
		TripAssignSrc: "auto", TripAssignState: "no_match", DedupStatus: "ok",
	}
	if err := paymentRepo.Create(payment); err != nil {
		t.Fatalf("创建支付失败: %v", err)
	}
	assertStoredMoney(t, db, "payments", "payment-1", "amount", "amount_cents", 10.24, 1024)

	if err := paymentRepo.UpdateForOwner("owner-1", "payment-1", map[string]any{"amount": 20.105}); err != nil {
		t.Fatalf("更新支付金额失败: %v", err)
	}
	assertStoredMoney(t, db, "payments", "payment-1", "amount", "amount_cents", 20.11, 2011)

	loadedPayment, err := paymentRepo.FindByIDForOwner("owner-1", "payment-1")
	if err != nil {
		t.Fatalf("读取支付失败: %v", err)
	}
	if loadedPayment.Amount != 20.11 || loadedPayment.AmountCents != 2011 {
		t.Fatalf("支付读取未以分字段为准: amount=%v cents=%d", loadedPayment.Amount, loadedPayment.AmountCents)
	}

	secondPayment := &models.Payment{
		ID: "payment-2", OwnerUserID: "owner-1", Amount: 0.2,
		TransactionTime: "2026-01-02T03:05:05Z", TransactionTimeTs: 2,
		TripAssignSrc: "auto", TripAssignState: "no_match", DedupStatus: "ok",
	}
	if err := paymentRepo.Create(secondPayment); err != nil {
		t.Fatalf("创建第二笔支付失败: %v", err)
	}
	paymentStats, err := paymentRepo.GetStatsByTs("owner-1", 0, 0)
	if err != nil {
		t.Fatalf("读取支付统计失败: %v", err)
	}
	if paymentStats.TotalAmount != 20.31 {
		t.Fatalf("支付统计金额错误: %v", paymentStats.TotalAmount)
	}

	amount := 30.345
	taxAmount := 1.235
	invoiceRepo := NewInvoiceRepository(db)
	invoice := &models.Invoice{
		ID: "invoice-1", OwnerUserID: "owner-1", Filename: "invoice.pdf",
		OriginalName: "invoice.pdf", FilePath: "uploads/invoice.pdf",
		Amount: &amount, TaxAmount: &taxAmount, ParseStatus: "success", Source: "upload", DedupStatus: "ok",
	}
	if err := invoiceRepo.Create(invoice); err != nil {
		t.Fatalf("创建发票失败: %v", err)
	}
	assertStoredMoney(t, db, "invoices", "invoice-1", "amount", "amount_cents", 30.35, 3035)
	assertStoredMoney(t, db, "invoices", "invoice-1", "tax_amount", "tax_amount_cents", 1.24, 124)

	if err := invoiceRepo.UpdateForOwner("owner-1", "invoice-1", map[string]any{"amount": 40.999, "tax_amount": nil}); err != nil {
		t.Fatalf("更新发票金额失败: %v", err)
	}
	assertStoredMoney(t, db, "invoices", "invoice-1", "amount", "amount_cents", 41, 4100)
	var nullTaxCount int64
	if err := db.Raw(`SELECT COUNT(*) FROM invoices WHERE id = ? AND tax_amount IS NULL AND tax_amount_cents IS NULL`, "invoice-1").Scan(&nullTaxCount).Error; err != nil {
		t.Fatalf("读取空税额失败: %v", err)
	}
	if nullTaxCount != 1 {
		t.Fatal("发票空税额未同步到元/分双列")
	}

	invoiceStats, err := invoiceRepo.GetStats("owner-1", "", "")
	if err != nil {
		t.Fatalf("读取发票统计失败: %v", err)
	}
	if invoiceStats.TotalAmount != 41 {
		t.Fatalf("发票统计金额错误: %v", invoiceStats.TotalAmount)
	}
}

func openMoneyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("打开金额测试数据库失败: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层数据库失败: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("关闭金额测试数据库失败: %v", err)
		}
	})
	return db
}

func assertStoredMoney(t *testing.T, db *gorm.DB, table, id, majorColumn, centsColumn string, wantMajor float64, wantCents int64) {
	t.Helper()
	type row struct {
		Major float64 `gorm:"column:major"`
		Cents int64   `gorm:"column:cents"`
	}
	var stored row
	query := fmt.Sprintf("SELECT %s AS major, %s AS cents FROM %s WHERE id = ?", majorColumn, centsColumn, table)
	if err := db.Raw(query, id).Scan(&stored).Error; err != nil {
		t.Fatalf("读取 %s 金额失败: %v", table, err)
	}
	if stored.Major != wantMajor || stored.Cents != wantCents {
		t.Fatalf("%s 金额双列不一致: major=%v cents=%d", table, stored.Major, stored.Cents)
	}
}
