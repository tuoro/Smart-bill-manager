package services

import (
	"log"
	"strings"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
)

// EnsureEmailLogUniqueIndex best-effort deduplicates email_logs and creates a unique index on
// (owner_user_id, email_config_id, mailbox, message_uid).
//
// We avoid relying on GORM's uniqueIndex tags because AutoMigrate would fail hard on existing
// installations that already have duplicates.
func EnsureEmailLogUniqueIndex(db *gorm.DB) {
	if db == nil {
		return
	}

	type dupGroup struct {
		OwnerUserID   string `gorm:"column:owner_user_id"`
		EmailConfigID string `gorm:"column:email_config_id"`
		Mailbox       string `gorm:"column:mailbox"`
		MessageUID    int64  `gorm:"column:message_uid"`
		Cnt           int64  `gorm:"column:cnt"`
	}

	var groups []dupGroup
	if err := db.Raw(`
		SELECT owner_user_id, email_config_id, mailbox, message_uid, COUNT(*) AS cnt
		FROM email_logs
		GROUP BY owner_user_id, email_config_id, mailbox, message_uid
		HAVING cnt > 1
	`).Scan(&groups).Error; err != nil {
		log.Printf("[Email Monitor] dedupe query failed: %v", err)
	} else if len(groups) > 0 {
		for _, g := range groups {
			owner := strings.TrimSpace(g.OwnerUserID)
			cfg := strings.TrimSpace(g.EmailConfigID)
			box := strings.TrimSpace(g.Mailbox)
			if owner == "" || cfg == "" || box == "" || g.MessageUID <= 0 {
				continue
			}

			var rows []models.EmailLog
			if err := db.
				Model(&models.EmailLog{}).
				Where("owner_user_id = ? AND email_config_id = ? AND mailbox = ? AND message_uid = ?", owner, cfg, box, g.MessageUID).
				Order("created_at DESC").
				Find(&rows).Error; err != nil || len(rows) <= 1 {
				continue
			}

			score := func(r models.EmailLog) (int, int64) {
				s := 0
				st := strings.ToLower(strings.TrimSpace(r.Status))
				if st == "parsed" {
					s += 200
				} else if st == "parsing" {
					s += 80
				} else if st == "error" {
					s += 20
				} else if st == "received" {
					s += 10
				}
				if r.ParsedInvoiceID != nil && strings.TrimSpace(*r.ParsedInvoiceID) != "" {
					s += 150
				}
				if r.ParsedInvoiceIDs != nil && strings.TrimSpace(*r.ParsedInvoiceIDs) != "" {
					s += 150
				}
				if r.InvoiceXMLURL != nil && strings.TrimSpace(*r.InvoiceXMLURL) != "" {
					s += 15
				}
				if r.InvoicePDFURL != nil && strings.TrimSpace(*r.InvoicePDFURL) != "" {
					s += 15
				}
				if r.HasAttachment != 0 {
					s += 5
				}
				if r.AttachmentCount > 0 {
					s += r.AttachmentCount
				}
				if r.ReceivedDate != nil && strings.TrimSpace(*r.ReceivedDate) != "" {
					s += 2
				}
				if r.Subject != nil && strings.TrimSpace(*r.Subject) != "" {
					s += 1
				}
				return s, r.CreatedAt.UnixNano()
			}

			bestIdx := 0
			bestScore, bestTS := score(rows[0])
			for i := 1; i < len(rows); i++ {
				sc, ts := score(rows[i])
				if sc > bestScore || (sc == bestScore && ts > bestTS) {
					bestIdx = i
					bestScore, bestTS = sc, ts
				}
			}
			best := rows[bestIdx]

			updates := map[string]any{}
			for i := range rows {
				if rows[i].ID == best.ID {
					continue
				}
				other := rows[i]

				// Merge "missing" fields into the best row so we don't lose parsed info / urls / metadata.
				if (best.Subject == nil || strings.TrimSpace(*best.Subject) == "") && other.Subject != nil && strings.TrimSpace(*other.Subject) != "" {
					updates["subject"] = strings.TrimSpace(*other.Subject)
					v := strings.TrimSpace(*other.Subject)
					best.Subject = &v
				}
				if (best.FromAddress == nil || strings.TrimSpace(*best.FromAddress) == "") && other.FromAddress != nil && strings.TrimSpace(*other.FromAddress) != "" {
					updates["from_address"] = strings.TrimSpace(*other.FromAddress)
					v := strings.TrimSpace(*other.FromAddress)
					best.FromAddress = &v
				}
				if (best.ReceivedDate == nil || strings.TrimSpace(*best.ReceivedDate) == "") && other.ReceivedDate != nil && strings.TrimSpace(*other.ReceivedDate) != "" {
					updates["received_date"] = strings.TrimSpace(*other.ReceivedDate)
					v := strings.TrimSpace(*other.ReceivedDate)
					best.ReceivedDate = &v
				}
				if best.HasAttachment < other.HasAttachment {
					updates["has_attachment"] = other.HasAttachment
					best.HasAttachment = other.HasAttachment
				}
				if best.AttachmentCount < other.AttachmentCount {
					updates["attachment_count"] = other.AttachmentCount
					best.AttachmentCount = other.AttachmentCount
				}
				if (best.InvoiceXMLURL == nil || strings.TrimSpace(*best.InvoiceXMLURL) == "") && other.InvoiceXMLURL != nil && strings.TrimSpace(*other.InvoiceXMLURL) != "" {
					updates["invoice_xml_url"] = strings.TrimSpace(*other.InvoiceXMLURL)
					v := strings.TrimSpace(*other.InvoiceXMLURL)
					best.InvoiceXMLURL = &v
				}
				if (best.InvoicePDFURL == nil || strings.TrimSpace(*best.InvoicePDFURL) == "") && other.InvoicePDFURL != nil && strings.TrimSpace(*other.InvoicePDFURL) != "" {
					updates["invoice_pdf_url"] = strings.TrimSpace(*other.InvoicePDFURL)
					v := strings.TrimSpace(*other.InvoicePDFURL)
					best.InvoicePDFURL = &v
				}
				if (best.ParsedInvoiceID == nil || strings.TrimSpace(*best.ParsedInvoiceID) == "") && other.ParsedInvoiceID != nil && strings.TrimSpace(*other.ParsedInvoiceID) != "" {
					updates["parsed_invoice_id"] = strings.TrimSpace(*other.ParsedInvoiceID)
					v := strings.TrimSpace(*other.ParsedInvoiceID)
					best.ParsedInvoiceID = &v
				}
				if (best.ParsedInvoiceIDs == nil || strings.TrimSpace(*best.ParsedInvoiceIDs) == "") && other.ParsedInvoiceIDs != nil && strings.TrimSpace(*other.ParsedInvoiceIDs) != "" {
					updates["parsed_invoice_ids"] = strings.TrimSpace(*other.ParsedInvoiceIDs)
					v := strings.TrimSpace(*other.ParsedInvoiceIDs)
					best.ParsedInvoiceIDs = &v
				}
				if (best.ParseError == nil || strings.TrimSpace(*best.ParseError) == "") && other.ParseError != nil && strings.TrimSpace(*other.ParseError) != "" {
					updates["parse_error"] = strings.TrimSpace(*other.ParseError)
					v := strings.TrimSpace(*other.ParseError)
					best.ParseError = &v
				}
				if strings.TrimSpace(best.Status) == "" && strings.TrimSpace(other.Status) != "" {
					updates["status"] = strings.TrimSpace(other.Status)
					best.Status = strings.TrimSpace(other.Status)
				}
			}

			if len(updates) > 0 {
				if err := db.Model(&models.EmailLog{}).Where("id = ?", best.ID).Updates(updates).Error; err != nil {
					log.Printf("[Email Monitor] dedupe merge update failed: id=%s err=%v", best.ID, err)
				}
			}

			delIDs := make([]string, 0, len(rows)-1)
			for _, r := range rows {
				if r.ID != best.ID {
					delIDs = append(delIDs, r.ID)
				}
			}
			if len(delIDs) > 0 {
				if err := db.Where("id IN ?", delIDs).Delete(&models.EmailLog{}).Error; err != nil {
					log.Printf("[Email Monitor] dedupe delete failed: keep=%s delete=%d err=%v", best.ID, len(delIDs), err)
				} else {
					log.Printf("[Email Monitor] dedupe email_logs: merged=%d keep=%s (config=%s uid=%d)", len(delIDs), best.ID, cfg, g.MessageUID)
				}
			}
		}
	}

	// Create unique index; ignore errors so startup never fails because of unexpected legacy data.
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_email_logs_owner_cfg_box_uid
		ON email_logs(owner_user_id, email_config_id, mailbox, message_uid)
	`).Error; err != nil {
		log.Printf("[Email Monitor] create unique index failed (ignored): %v", err)
	}
}

