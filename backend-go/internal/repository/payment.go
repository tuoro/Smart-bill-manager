package repository

import (
	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

type PaymentRepository struct{}

func NewPaymentRepository() *PaymentRepository {
	return &PaymentRepository{}
}

func (r *PaymentRepository) Create(payment *models.Payment) error {
	return database.GetDB().Create(payment).Error
}

func (r *PaymentRepository) FindByID(id string) (*models.Payment, error) {
	var payment models.Payment
	err := database.GetDB().Where("id = ?", id).First(&payment).Error
	if err != nil {
		return nil, err
	}
	return &payment, nil
}

type PaymentFilter struct {
	Limit     int
	Offset    int
	StartDate string
	EndDate   string
	StartTs   int64
	EndTs     int64
	Category  string
	// IncludeDraft controls whether draft records are included.
	// By default, drafts are hidden from normal list/stats flows.
	IncludeDraft bool
}

func (r *PaymentRepository) FindAll(filter PaymentFilter) ([]models.Payment, error) {
	var payments []models.Payment

	query := database.GetDB().Model(&models.Payment{})
	if !filter.IncludeDraft {
		query = query.Where("is_draft = 0")
	}

	if filter.StartTs > 0 {
		query = query.Where("transaction_time_ts >= ?", filter.StartTs)
	}
	if filter.EndTs > 0 {
		query = query.Where("transaction_time_ts <= ?", filter.EndTs)
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}

	query = query.Order("transaction_time_ts DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
		if filter.Offset > 0 {
			query = query.Offset(filter.Offset)
		}
	}

	err := query.Find(&payments).Error
	return payments, err
}

func (r *PaymentRepository) Update(id string, data map[string]interface{}) error {
	result := database.GetDB().Model(&models.Payment{}).Where("id = ?", id).Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *PaymentRepository) Delete(id string) error {
	result := database.GetDB().Where("id = ?", id).Delete(&models.Payment{})
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *PaymentRepository) GetStats(startDate, endDate string) (*models.PaymentStats, error) {
	var payments []models.Payment

	query := database.GetDB().Model(&models.Payment{}).Where("is_draft = 0")

	if startDate != "" {
		query = query.Where("transaction_time >= ?", startDate)
	}
	if endDate != "" {
		query = query.Where("transaction_time <= ?", endDate)
	}

	if err := query.Find(&payments).Error; err != nil {
		return nil, err
	}

	stats := &models.PaymentStats{
		CategoryStats: make(map[string]float64),
		MerchantStats: make(map[string]float64),
		DailyStats:    make(map[string]float64),
	}

	for _, p := range payments {
		stats.TotalAmount += p.Amount
		stats.TotalCount++

		category := "未分类"
		if p.Category != nil && *p.Category != "" {
			category = *p.Category
		}
		stats.CategoryStats[category] += p.Amount

		merchant := "未知商家"
		if p.Merchant != nil && *p.Merchant != "" {
			merchant = *p.Merchant
		}
		stats.MerchantStats[merchant] += p.Amount

		if len(p.TransactionTime) >= 10 {
			date := p.TransactionTime[:10]
			stats.DailyStats[date] += p.Amount
		}
	}

	return stats, nil
}

// GetStatsByTs uses SQL aggregation to compute stats efficiently.
// startTs/endTs are UTC unix milliseconds; 0 means unbounded.
func (r *PaymentRepository) GetStatsByTs(startTs, endTs int64) (*models.PaymentStats, error) {
	applyFilter := func(q *gorm.DB) *gorm.DB {
		q = q.Where("is_draft = 0")
		if startTs > 0 {
			q = q.Where("transaction_time_ts >= ?", startTs)
		}
		if endTs > 0 {
			q = q.Where("transaction_time_ts <= ?", endTs)
		}
		return q
	}

	stats := &models.PaymentStats{
		CategoryStats: make(map[string]float64),
		MerchantStats: make(map[string]float64),
		DailyStats:    make(map[string]float64),
	}

	type totalsRow struct {
		TotalAmount float64 `gorm:"column:total_amount"`
		TotalCount  int64   `gorm:"column:total_count"`
	}
	var totals totalsRow
	if err := applyFilter(database.GetDB().Table("payments")).
		Select("COALESCE(SUM(amount), 0) AS total_amount, COUNT(*) AS total_count").
		Scan(&totals).Error; err != nil {
		return nil, err
	}
	stats.TotalAmount = totals.TotalAmount
	stats.TotalCount = int(totals.TotalCount)

	type kvRow struct {
		Key   string  `gorm:"column:k"`
		Total float64 `gorm:"column:total"`
	}

	// Category stats
	var catRows []kvRow
	if err := applyFilter(database.GetDB().Table("payments")).
		Select(`CASE WHEN category IS NULL OR TRIM(category) = '' THEN '未分类' ELSE category END AS k, COALESCE(SUM(amount), 0) AS total`).
		Group("k").
		Scan(&catRows).Error; err != nil {
		return nil, err
	}
	for _, r := range catRows {
		stats.CategoryStats[r.Key] = r.Total
	}

	// Merchant stats
	var merchRows []kvRow
	if err := applyFilter(database.GetDB().Table("payments")).
		Select(`CASE WHEN merchant IS NULL OR TRIM(merchant) = '' THEN '未知商家' ELSE merchant END AS k, COALESCE(SUM(amount), 0) AS total`).
		Group("k").
		Scan(&merchRows).Error; err != nil {
		return nil, err
	}
	for _, r := range merchRows {
		stats.MerchantStats[r.Key] = r.Total
	}

	// Daily stats (YYYY-MM-DD from RFC3339 string)
	var dayRows []kvRow
	if err := applyFilter(database.GetDB().Table("payments")).
		Select(`SUBSTR(transaction_time, 1, 10) AS k, COALESCE(SUM(amount), 0) AS total`).
		Group("k").
		Scan(&dayRows).Error; err != nil {
		return nil, err
	}
	for _, r := range dayRows {
		if len(r.Key) == 10 {
			stats.DailyStats[r.Key] = r.Total
		}
	}

	return stats, nil
}

// GetLinkedInvoices returns all invoices linked to a payment
func (r *PaymentRepository) GetLinkedInvoices(paymentID string) ([]models.Invoice, error) {
	var invoices []models.Invoice
	err := database.GetDB().
		Joins("INNER JOIN invoice_payment_links ON invoice_payment_links.invoice_id = invoices.id").
		Where("invoice_payment_links.payment_id = ?", paymentID).
		Where("invoices.is_draft = 0").
		Find(&invoices).Error
	return invoices, err
}
