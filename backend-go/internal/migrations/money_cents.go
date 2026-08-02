package migrations

import (
	"fmt"

	"smart-bill-manager/internal/money"

	"gorm.io/gorm"
)

func migrateMoneyCents(db *gorm.DB) error {
	if err := backfillPaymentMoney(db); err != nil {
		return err
	}
	if err := backfillInvoiceMoney(db); err != nil {
		return err
	}

	statements := []struct {
		name string
		sql  string
	}{
		{name: "创建支付金额分索引", sql: `CREATE INDEX IF NOT EXISTS idx_payments_amount_cents ON payments(amount_cents)`},
		{name: "创建发票金额分索引", sql: `CREATE INDEX IF NOT EXISTS idx_invoices_amount_cents ON invoices(amount_cents)`},
	}
	for _, statement := range statements {
		if err := execSQL(db, statement.name, statement.sql); err != nil {
			return err
		}
	}

	return validateMoneyColumns(db)
}

func backfillPaymentMoney(db *gorm.DB) error {
	type paymentRow struct {
		ID     string  `gorm:"column:id"`
		Amount float64 `gorm:"column:amount"`
	}
	var rows []paymentRow
	if err := db.Table("payments").Select("id", "amount").Scan(&rows).Error; err != nil {
		return fmt.Errorf("读取支付金额失败: %w", err)
	}
	for _, row := range rows {
		cents, err := money.FromMajor(row.Amount)
		if err != nil {
			return fmt.Errorf("转换支付 %s 金额失败: %w", row.ID, err)
		}
		if err := db.Table("payments").Where("id = ?", row.ID).Updates(map[string]any{
			"amount":       money.ToMajor(cents),
			"amount_cents": cents,
		}).Error; err != nil {
			return fmt.Errorf("回填支付 %s 金额分失败: %w", row.ID, err)
		}
	}
	return nil
}

func backfillInvoiceMoney(db *gorm.DB) error {
	type invoiceRow struct {
		ID        string   `gorm:"column:id"`
		Amount    *float64 `gorm:"column:amount"`
		TaxAmount *float64 `gorm:"column:tax_amount"`
	}
	var rows []invoiceRow
	if err := db.Table("invoices").Select("id", "amount", "tax_amount").Scan(&rows).Error; err != nil {
		return fmt.Errorf("读取发票金额失败: %w", err)
	}
	for _, row := range rows {
		amountCents, err := money.FromMajorPointer(row.Amount)
		if err != nil {
			return fmt.Errorf("转换发票 %s 金额失败: %w", row.ID, err)
		}
		taxCents, err := money.FromMajorPointer(row.TaxAmount)
		if err != nil {
			return fmt.Errorf("转换发票 %s 税额失败: %w", row.ID, err)
		}
		if err := db.Table("invoices").Where("id = ?", row.ID).Updates(map[string]any{
			"amount":           money.ToMajorPointer(amountCents),
			"amount_cents":     amountCents,
			"tax_amount":       money.ToMajorPointer(taxCents),
			"tax_amount_cents": taxCents,
		}).Error; err != nil {
			return fmt.Errorf("回填发票 %s 金额分失败: %w", row.ID, err)
		}
	}
	return nil
}

func validateMoneyColumns(db *gorm.DB) error {
	type paymentRow struct {
		ID          string  `gorm:"column:id"`
		Amount      float64 `gorm:"column:amount"`
		AmountCents int64   `gorm:"column:amount_cents"`
	}
	var payments []paymentRow
	if err := db.Table("payments").Select("id", "amount", "amount_cents").Scan(&payments).Error; err != nil {
		return fmt.Errorf("校验支付金额分失败: %w", err)
	}
	for _, row := range payments {
		want, err := money.FromMajor(row.Amount)
		if err != nil || want != row.AmountCents {
			return fmt.Errorf("校验支付金额分失败: 支付 %s 不一致", row.ID)
		}
	}

	type invoiceRow struct {
		ID             string   `gorm:"column:id"`
		Amount         *float64 `gorm:"column:amount"`
		AmountCents    *int64   `gorm:"column:amount_cents"`
		TaxAmount      *float64 `gorm:"column:tax_amount"`
		TaxAmountCents *int64   `gorm:"column:tax_amount_cents"`
	}
	var invoices []invoiceRow
	if err := db.Table("invoices").Select("id", "amount", "amount_cents", "tax_amount", "tax_amount_cents").Scan(&invoices).Error; err != nil {
		return fmt.Errorf("校验发票金额分失败: %w", err)
	}
	for _, row := range invoices {
		wantAmount, amountErr := money.FromMajorPointer(row.Amount)
		wantTax, taxErr := money.FromMajorPointer(row.TaxAmount)
		if amountErr != nil || taxErr != nil || !equalCents(wantAmount, row.AmountCents) || !equalCents(wantTax, row.TaxAmountCents) {
			return fmt.Errorf("校验发票金额分失败: 发票 %s 不一致", row.ID)
		}
	}
	return nil
}

func equalCents(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
