package repository

import (
	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

type InvoiceRepository struct{}

func NewInvoiceRepository() *InvoiceRepository {
	return &InvoiceRepository{}
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
}

func (r *InvoiceRepository) FindAll(filter InvoiceFilter) ([]models.Invoice, error) {
	var invoices []models.Invoice
	
	query := database.GetDB().Model(&models.Invoice{}).Order("created_at DESC")
	
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
	err := database.GetDB().Where("payment_id = ?", paymentID).Find(&invoices).Error
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
	var invoices []models.Invoice
	
	if err := database.GetDB().Find(&invoices).Error; err != nil {
		return nil, err
	}
	
	stats := &models.InvoiceStats{
		BySource: make(map[string]int),
		ByMonth:  make(map[string]float64),
	}
	
	for _, inv := range invoices {
		stats.TotalCount++
		if inv.Amount != nil {
			stats.TotalAmount += *inv.Amount
		}
		
		source := inv.Source
		if source == "" {
			source = "unknown"
		}
		stats.BySource[source]++
		
		if inv.InvoiceDate != nil && len(*inv.InvoiceDate) >= 7 {
			month := (*inv.InvoiceDate)[:7]
			if inv.Amount != nil {
				stats.ByMonth[month] += *inv.Amount
			}
		}
	}
	
	return stats, nil
}
