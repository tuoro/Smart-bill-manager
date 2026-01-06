package repository

import (
	"context"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"
	"strings"

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
	OwnerUserID string
	Limit       int
	Offset      int
	AfterTs     int64
	AfterID     string
	StartDate   string
	EndDate     string
	StartTs     int64
	EndTs       int64
	Category    string
	// IncludeDraft controls whether draft records are included.
	// By default, drafts are hidden from normal list/stats flows.
	IncludeDraft bool
}

func (r *PaymentRepository) buildFindAllQuery(ctx context.Context, filter PaymentFilter) *gorm.DB {
	if ctx == nil {
		ctx = context.Background()
	}
	query := database.GetDB().WithContext(ctx).Model(&models.Payment{})
	if filter.OwnerUserID != "" {
		query = query.Where("owner_user_id = ?", filter.OwnerUserID)
	}
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
	if filter.AfterTs > 0 && strings.TrimSpace(filter.AfterID) != "" {
		afterID := strings.TrimSpace(filter.AfterID)
		query = query.Where("(transaction_time_ts < ?) OR (transaction_time_ts = ? AND id < ?)", filter.AfterTs, filter.AfterTs, afterID)
	}
	return query
}

func (r *PaymentRepository) FindAll(filter PaymentFilter) ([]models.Payment, error) {
	var payments []models.Payment

	query := r.buildFindAllQuery(context.Background(), filter).Order("transaction_time_ts DESC, id DESC")

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
		if filter.Offset > 0 {
			query = query.Offset(filter.Offset)
		}
	}

	err := query.Find(&payments).Error
	return payments, err
}

func (r *PaymentRepository) FindAllPaged(filter PaymentFilter, selectCols []string) ([]models.Payment, int64, error) {
	return r.FindAllPagedCtx(context.Background(), filter, selectCols)
}

func (r *PaymentRepository) FindAllPagedCtx(ctx context.Context, filter PaymentFilter, selectCols []string) ([]models.Payment, int64, error) {
	var payments []models.Payment

	// total should represent the full filtered dataset, not the cursor window
	countFilter := filter
	countFilter.AfterTs = 0
	countFilter.AfterID = ""
	query := r.buildFindAllQuery(ctx, countFilter)
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = r.buildFindAllQuery(ctx, filter).Order("transaction_time_ts DESC, id DESC")
	if len(selectCols) > 0 {
		query = query.Select(selectCols)
	}
	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
		if filter.Offset > 0 {
			query = query.Offset(filter.Offset)
		}
	}

	if err := query.Find(&payments).Error; err != nil {
		return nil, 0, err
	}
	return payments, total, nil
}

func (r *PaymentRepository) FindByIDForOwner(ownerUserID string, id string) (*models.Payment, error) {
	return r.FindByIDForOwnerCtx(context.Background(), ownerUserID, id)
}

func (r *PaymentRepository) FindByIDForOwnerCtx(ctx context.Context, ownerUserID string, id string) (*models.Payment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var payment models.Payment
	q := database.GetDB().WithContext(ctx).Where("id = ?", id)
	if ownerUserID != "" {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	err := q.First(&payment).Error
	if err != nil {
		return nil, err
	}
	return &payment, nil
}

func (r *PaymentRepository) Update(id string, data map[string]interface{}) error {
	result := database.GetDB().Model(&models.Payment{}).Where("id = ?", id).Updates(data)
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func (r *PaymentRepository) UpdateForOwner(ownerUserID string, id string, data map[string]interface{}) error {
	q := database.GetDB().Model(&models.Payment{}).Where("id = ?", id)
	if ownerUserID != "" {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	result := q.Updates(data)
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

func (r *PaymentRepository) DeleteForOwner(ownerUserID string, id string) error {
	q := database.GetDB().Where("id = ?", id)
	if ownerUserID != "" {
		q = q.Where("owner_user_id = ?", ownerUserID)
	}
	result := q.Delete(&models.Payment{})
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
func (r *PaymentRepository) GetStatsByTs(ownerUserID string, startTs, endTs int64) (*models.PaymentStats, error) {
	return r.GetStatsByTsCtx(context.Background(), ownerUserID, startTs, endTs)
}

func (r *PaymentRepository) GetStatsByTsCtx(ctx context.Context, ownerUserID string, startTs, endTs int64) (*models.PaymentStats, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	applyFilter := func(q *gorm.DB) *gorm.DB {
		q = q.Where("is_draft = 0")
		if ownerUserID != "" {
			q = q.Where("owner_user_id = ?", ownerUserID)
		}
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
	if err := applyFilter(database.GetDB().WithContext(ctx).Table("payments")).
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
	if err := applyFilter(database.GetDB().WithContext(ctx).Table("payments")).
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
	if err := applyFilter(database.GetDB().WithContext(ctx).Table("payments")).
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
	if err := applyFilter(database.GetDB().WithContext(ctx).Table("payments")).
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
func (r *PaymentRepository) GetLinkedInvoices(ownerUserID string, paymentID string) ([]models.Invoice, error) {
	var invoices []models.Invoice
	q := database.GetDB().
		Joins("INNER JOIN invoice_payment_links ON invoice_payment_links.invoice_id = invoices.id").
		Where("invoice_payment_links.payment_id = ?", paymentID).
		Where("invoices.is_draft = 0")
	if ownerUserID != "" {
		q = q.Where("invoices.owner_user_id = ?", ownerUserID)
	}
	err := q.Find(&invoices).Error
	return invoices, err
}
