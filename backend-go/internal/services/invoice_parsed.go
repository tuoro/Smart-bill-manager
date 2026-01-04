package services

import (
	"encoding/json"
	"fmt"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/utils"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

// CreateFromExtracted creates an invoice without running OCR/PDF parsing.
// Intended for structured sources (e.g. invoice XML from email).
func (s *InvoiceService) CreateFromExtracted(ownerUserID string, input CreateInvoiceInput, extracted InvoiceExtractedData) (*models.Invoice, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil, fmt.Errorf("missing owner_user_id")
	}
	id := utils.GenerateUUID()

	if input.PaymentID != nil {
		pid := strings.TrimSpace(*input.PaymentID)
		if pid == "" {
			input.PaymentID = nil
		} else {
			input.PaymentID = &pid
		}
	}

	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "upload"
	}

	extractedBytes, err := json.Marshal(extracted)
	if err != nil {
		return nil, fmt.Errorf("marshal extracted_data: %w", err)
	}
	extractedStr := string(extractedBytes)

	invoiceNumber := extracted.InvoiceNumber
	invoiceDate := extracted.InvoiceDate
	amount := extracted.Amount
	taxAmount := extracted.TaxAmount
	sellerName := extracted.SellerName
	buyerName := extracted.BuyerName

	inv := &models.Invoice{
		ID:            id,
		OwnerUserID:   ownerUserID,
		IsDraft:       false,
		PaymentID:     input.PaymentID,
		Filename:      input.Filename,
		OriginalName:  input.OriginalName,
		FilePath:      input.FilePath,
		FileSize:      &input.FileSize,
		FileSHA256:    input.FileSHA256,
		InvoiceNumber: invoiceNumber,
		InvoiceDate:   invoiceDate,
		Amount:        amount,
		TaxAmount:     taxAmount,
		SellerName:    sellerName,
		BuyerName:     buyerName,
		ExtractedData: &extractedStr,
		ParseStatus:   "success",
		ParseError:    nil,
		RawText:       nil,
		Source:        source,
		DedupStatus:   DedupStatusOK,
	}

	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(inv).Error; err != nil {
			return err
		}
		if input.PaymentID != nil {
			pid := strings.TrimSpace(*input.PaymentID)
			if pid != "" {
				var pay models.Payment
				if err := tx.Select("id").Where("id = ? AND owner_user_id = ? AND is_draft = 0", pid, ownerUserID).First(&pay).Error; err != nil {
					return fmt.Errorf("payment not found")
				}
				if err := tx.Table("invoice_payment_links").Create(&models.InvoicePaymentLink{
					InvoiceID: inv.ID,
					PaymentID: pid,
				}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}

	// Mark suspected duplicates (invoice_number).
	if inv.InvoiceNumber != nil {
		n := strings.TrimSpace(*inv.InvoiceNumber)
		if n != "" {
			if cands, err := FindInvoiceCandidatesByInvoiceNumber(n, inv.ID, 5); err == nil && len(cands) > 0 {
				inv.DedupStatus = DedupStatusSuspected
				ref := cands[0].ID
				inv.DedupRefID = &ref
				_ = db.Model(&models.Invoice{}).Where("id = ?", inv.ID).Updates(map[string]interface{}{
					"dedup_status": DedupStatusSuspected,
					"dedup_ref_id": ref,
				}).Error
			}
		}
	}

	return inv, nil
}
