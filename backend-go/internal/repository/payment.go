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
	Category  string
}

func (r *PaymentRepository) FindAll(filter PaymentFilter) ([]models.Payment, error) {
	var payments []models.Payment

	query := database.GetDB().Model(&models.Payment{})

	if filter.StartDate != "" {
		query = query.Where("transaction_time >= ?", filter.StartDate)
	}
	if filter.EndDate != "" {
		query = query.Where("transaction_time <= ?", filter.EndDate)
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}

	query = query.Order("transaction_time DESC")

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

	query := database.GetDB().Model(&models.Payment{})

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

// GetLinkedInvoices returns all invoices linked to a payment
func (r *PaymentRepository) GetLinkedInvoices(paymentID string) ([]models.Invoice, error) {
	var invoices []models.Invoice
	err := database.GetDB().
		Joins("INNER JOIN invoice_payment_links ON invoice_payment_links.invoice_id = invoices.id").
		Where("invoice_payment_links.payment_id = ?", paymentID).
		Find(&invoices).Error
	return invoices, err
}
