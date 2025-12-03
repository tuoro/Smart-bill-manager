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

// LinkPayment creates a link between an invoice and a payment
func (r *InvoiceRepository) LinkPayment(invoiceID, paymentID string) error {
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
		Find(&payments).Error
	return payments, err
}

// SuggestPayments suggests payments that might match an invoice
func (r *InvoiceRepository) SuggestPayments(invoice *models.Invoice, limit int) ([]models.Payment, error) {
	var payments []models.Payment
	
	query := database.GetDB().Model(&models.Payment{})
	
	// If invoice has amount, filter by similar amounts (within 10% range)
	if invoice.Amount != nil {
		minAmount := *invoice.Amount * 0.9
		maxAmount := *invoice.Amount * 1.1
		query = query.Where("amount >= ? AND amount <= ?", minAmount, maxAmount)
	}
	
	// If invoice has date, prioritize payments from similar timeframe
	if invoice.InvoiceDate != nil && len(*invoice.InvoiceDate) >= 10 {
		// Extract date part (first 10 characters: YYYY-MM-DD)
		dateStr := (*invoice.InvoiceDate)[:10]
		query = query.Where("transaction_time LIKE ?", dateStr+"%")
	}
	
	// Order by closest amount match
	if invoice.Amount != nil {
		query = query.Order(gorm.Expr("ABS(amount - ?) ASC", *invoice.Amount))
	}
	
	if limit > 0 {
		query = query.Limit(limit)
	}
	
	err := query.Find(&payments).Error
	return payments, err
}
