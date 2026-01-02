package repository

import (
	"fmt"
	"math"
	"regexp"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"
	"strconv"

	"gorm.io/gorm"
)

type InvoiceRepository struct{}

func NewInvoiceRepository() *InvoiceRepository {
	return &InvoiceRepository{}
}

var invoiceDatePrefixRegex = regexp.MustCompile(`(\d{4})\D+(\d{1,2})\D+(\d{1,2})`)

func normalizeDatePrefix(s string) string {
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		// Likely YYYY-MM-DD...
		return s[:10]
	}
	if len(s) >= 10 && s[4] == '/' && s[7] == '/' {
		// Likely YYYY/MM/DD...
		return s[:10]
	}
	if m := invoiceDatePrefixRegex.FindStringSubmatch(s); len(m) == 4 {
		month, err1 := strconv.Atoi(m[2])
		day, err2 := strconv.Atoi(m[3])
		if err1 != nil || err2 != nil || month < 1 || month > 12 || day < 1 || day > 31 {
			return ""
		}
		return fmt.Sprintf("%s-%02d-%02d", m[1], month, day)
	}
	return ""
}

func (r *InvoiceRepository) Create(invoice *models.Invoice) error {
	return database.GetDB().Create(invoice).Error
}

func (r *InvoiceRepository) FindByID(id string) (*models.Invoice, error) {
	var invoice models.Invoice
	err := database.GetDB().Where("id = ?", id).First(&invoice).Error
	if err != nil {
		return nil, err
	}
	return &invoice, nil
}

type InvoiceFilter struct {
	Limit  int
	Offset int
	// IncludeDraft controls whether draft records are included.
	// By default, drafts are hidden from normal list/stats flows.
	IncludeDraft bool
}

func (r *InvoiceRepository) FindAll(filter InvoiceFilter) ([]models.Invoice, error) {
	var invoices []models.Invoice

	query := database.GetDB().Model(&models.Invoice{}).Order("created_at DESC")
	if !filter.IncludeDraft {
		query = query.Where("is_draft = 0")
	}

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
		if filter.Offset > 0 {
			query = query.Offset(filter.Offset)
		}
	}

	err := query.Find(&invoices).Error
	return invoices, err
}

func (r *InvoiceRepository) FindByPaymentID(paymentID string) ([]models.Invoice, error) {
	var invoices []models.Invoice
	err := database.GetDB().Where("payment_id = ?", paymentID).Where("is_draft = 0").Find(&invoices).Error
	return invoices, err
}

func (r *InvoiceRepository) Update(id string, data map[string]interface{}) error {
	result := database.GetDB().Model(&models.Invoice{}).Where("id = ?", id).Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *InvoiceRepository) Delete(id string) error {
	result := database.GetDB().Where("id = ?", id).Delete(&models.Invoice{})
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *InvoiceRepository) GetStats() (*models.InvoiceStats, error) {
	stats := &models.InvoiceStats{
		BySource: make(map[string]int),
		ByMonth:  make(map[string]float64),
	}

	type totalsRow struct {
		TotalCount  int64   `gorm:"column:total_count"`
		TotalAmount float64 `gorm:"column:total_amount"`
	}
	var totals totalsRow
	if err := database.GetDB().
		Table("invoices").
		Where("is_draft = 0").
		Select("COUNT(*) AS total_count, COALESCE(SUM(amount), 0) AS total_amount").
		Scan(&totals).Error; err != nil {
		return nil, err
	}
	stats.TotalCount = int(totals.TotalCount)
	stats.TotalAmount = totals.TotalAmount

	// By source
	type srcRow struct {
		Source string `gorm:"column:src"`
		Cnt    int64  `gorm:"column:cnt"`
	}
	var srcRows []srcRow
	if err := database.GetDB().
		Table("invoices").
		Where("is_draft = 0").
		Select(`CASE WHEN source IS NULL OR TRIM(source) = '' THEN 'unknown' ELSE source END AS src, COUNT(*) AS cnt`).
		Group("src").
		Scan(&srcRows).Error; err != nil {
		return nil, err
	}
	for _, r := range srcRows {
		stats.BySource[r.Source] = int(r.Cnt)
	}

	// By month (YYYY-MM)
	type monthRow struct {
		Month string  `gorm:"column:m"`
		Total float64 `gorm:"column:total"`
	}
	var monthRows []monthRow
	if err := database.GetDB().
		Table("invoices").
		Where("is_draft = 0").
		Where("invoice_date IS NOT NULL AND LENGTH(invoice_date) >= 7 AND amount IS NOT NULL").
		Select(`SUBSTR(invoice_date, 1, 7) AS m, COALESCE(SUM(amount), 0) AS total`).
		Group("m").
		Scan(&monthRows).Error; err != nil {
		return nil, err
	}
	for _, r := range monthRows {
		if len(r.Month) == 7 {
			stats.ByMonth[r.Month] = r.Total
		}
	}

	return stats, nil
}

// LinkPayment creates a link between an invoice and a payment
func (r *InvoiceRepository) LinkPayment(invoiceID, paymentID string) error {
	// Enforce invoice -> 0/1 payment (DB has a unique index on invoice_id).
	var cnt int64
	if err := database.GetDB().Table("invoice_payment_links").Where("invoice_id = ?", invoiceID).Count(&cnt).Error; err != nil {
		return err
	}
	if cnt > 0 {
		return fmt.Errorf("invoice already linked to a payment")
	}

	link := &models.InvoicePaymentLink{
		InvoiceID: invoiceID,
		PaymentID: paymentID,
	}
	return database.GetDB().Create(link).Error
}

// UnlinkPayment removes the link between an invoice and a payment
func (r *InvoiceRepository) UnlinkPayment(invoiceID, paymentID string) error {
	return database.GetDB().Where("invoice_id = ? AND payment_id = ?", invoiceID, paymentID).
		Delete(&models.InvoicePaymentLink{}).Error
}

// GetLinkedPayments returns all payments linked to an invoice
func (r *InvoiceRepository) GetLinkedPayments(invoiceID string) ([]models.Payment, error) {
	var payments []models.Payment
	err := database.GetDB().
		Joins("INNER JOIN invoice_payment_links ON invoice_payment_links.payment_id = payments.id").
		Where("invoice_payment_links.invoice_id = ?", invoiceID).
		Where("payments.is_draft = 0").
		Find(&payments).Error
	return payments, err
}

// SuggestPayments suggests payments that might match an invoice
func (r *InvoiceRepository) SuggestPayments(invoice *models.Invoice, limit int) ([]models.Payment, error) {
	var payments []models.Payment

	base := database.GetDB().Model(&models.Payment{})
	baseNoAmount := database.GetDB().Model(&models.Payment{})
	base = base.Where("is_draft = 0")
	baseNoAmount = baseNoAmount.Where("is_draft = 0")

	// If invoice has amount, filter by similar amounts (within 10% range)
	if invoice.Amount != nil {
		minAmount := *invoice.Amount * 0.8
		maxAmount := *invoice.Amount * 1.2
		// Support negative payment amounts by matching on absolute value.
		base = base.Where("ABS(amount) >= ? AND ABS(amount) <= ?", minAmount, maxAmount)
	}

	// If invoice has date, prioritize payments from similar timeframe
	datePrefix := ""
	if invoice.InvoiceDate != nil && *invoice.InvoiceDate != "" {
		datePrefix = normalizeDatePrefix(*invoice.InvoiceDate)
	}

	// Default: newest first (service will apply scoring on top)
	withOrder := func(q *gorm.DB, hasAmount bool, amount float64) *gorm.DB {
		if hasAmount {
			// Prefer closest amounts even if the record is older.
			q = q.Order(gorm.Expr("ABS(ABS(amount) - ?) ASC", amount))
		}
		return q.Order("transaction_time DESC")
	}
	hasInvoiceAmount := invoice.Amount != nil && *invoice.Amount > 0
	invoiceAmount := 0.0
	if hasInvoiceAmount {
		invoiceAmount = *invoice.Amount
	}

	if limit > 0 {
		base = base.Limit(limit)
		baseNoAmount = baseNoAmount.Limit(limit)
	}

	// First try: amount (+ date if available).
	q := base
	if datePrefix != "" {
		q = q.Where("transaction_time LIKE ?", datePrefix+"%")
	}
	err := withOrder(q, hasInvoiceAmount, invoiceAmount).Find(&payments).Error
	if err != nil {
		return nil, err
	}

	// Fallback: if date filter is too strict (e.g. payment transaction_time missing),
	// retry without date constraint so scoring can still rank by proximity.
	if len(payments) == 0 && datePrefix != "" {
		payments = nil
		if err := withOrder(base, hasInvoiceAmount, invoiceAmount).Find(&payments).Error; err != nil {
			return nil, err
		}
	}

	// Fallback: if amount filter is too strict (e.g. payment amount not extracted and stored as 0),
	// retry without amount constraint (still bounded by limit).
	if len(payments) == 0 && invoice.Amount != nil {
		q2 := baseNoAmount
		if datePrefix != "" {
			q2 = q2.Where("transaction_time LIKE ?", datePrefix+"%")
		}
		if err := withOrder(q2, hasInvoiceAmount, invoiceAmount).Find(&payments).Error; err != nil {
			return nil, err
		}
		if len(payments) == 0 && datePrefix != "" {
			payments = nil
			if err := withOrder(baseNoAmount, hasInvoiceAmount, invoiceAmount).Find(&payments).Error; err != nil {
				return nil, err
			}
		}
	}

	return payments, nil
}

// SuggestInvoices suggests invoices that might match a payment (used by payment-side recommendations).
func (r *InvoiceRepository) SuggestInvoices(payment *models.Payment, limit int) ([]models.Invoice, error) {
	var invoices []models.Invoice

	query := database.GetDB().Model(&models.Invoice{}).Where("is_draft = 0")
	absAmount := 0.0
	hasAmount := false

	if payment != nil && payment.Amount != 0 {
		absAmount = math.Abs(payment.Amount)
		hasAmount = absAmount > 0
		minAmount := absAmount * 0.8
		maxAmount := absAmount * 1.2
		query = query.Where("amount >= ? AND amount <= ?", minAmount, maxAmount)
	}

	if hasAmount {
		query = query.Order(gorm.Expr("ABS(amount - ?) ASC", absAmount))
	}
	query = query.Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	err := query.Find(&invoices).Error
	return invoices, err
}
