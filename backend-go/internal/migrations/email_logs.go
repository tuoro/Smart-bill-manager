package migrations

import (
	"fmt"
	"strings"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

const emailLogIdentityIndex = "idx_email_logs_owner_cfg_box_uid"

type duplicateEmailLogGroup struct {
	OwnerUserID   string `gorm:"column:owner_user_id"`
	EmailConfigID string `gorm:"column:email_config_id"`
	Mailbox       string `gorm:"column:mailbox"`
	MessageUID    uint32 `gorm:"column:message_uid"`
}

func migrateEmailLogIdentity(db *gorm.DB) error {
	var groups []duplicateEmailLogGroup
	if err := db.Raw(`
		SELECT owner_user_id, email_config_id, mailbox, message_uid
		FROM email_logs
		GROUP BY owner_user_id, email_config_id, mailbox, message_uid
		HAVING COUNT(*) > 1
		ORDER BY owner_user_id, email_config_id, mailbox, message_uid
	`).Scan(&groups).Error; err != nil {
		return fmt.Errorf("读取邮件日志重复组失败: %w", err)
	}

	for _, group := range groups {
		if err := deduplicateEmailLogGroup(db, group); err != nil {
			return err
		}
	}

	if err := execSQL(db, "创建邮件日志身份唯一索引", `
		CREATE UNIQUE INDEX IF NOT EXISTS idx_email_logs_owner_cfg_box_uid
		ON email_logs(owner_user_id, email_config_id, mailbox, message_uid)
	`); err != nil {
		return err
	}
	return validateEmailLogIdentityIndex(db)
}

func deduplicateEmailLogGroup(db *gorm.DB, group duplicateEmailLogGroup) error {
	var rows []models.EmailLog
	if err := db.
		Where(
			"owner_user_id = ? AND email_config_id = ? AND mailbox = ? AND message_uid = ?",
			group.OwnerUserID,
			group.EmailConfigID,
			group.Mailbox,
			group.MessageUID,
		).
		Order("created_at DESC").
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return fmt.Errorf("读取邮件日志重复记录失败: %w", err)
	}
	if len(rows) <= 1 {
		return nil
	}

	best := selectBestEmailLog(rows)
	updates := mergeEmailLogFields(&best, rows)
	if len(updates) > 0 {
		result := db.Model(&models.EmailLog{}).Where("id = ?", best.ID).Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("合并邮件日志重复记录失败: id=%s: %w", best.ID, result.Error)
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("合并邮件日志重复记录失败: id=%s 更新行数=%d", best.ID, result.RowsAffected)
		}
	}

	duplicateIDs := make([]string, 0, len(rows)-1)
	for _, row := range rows {
		if row.ID != best.ID {
			duplicateIDs = append(duplicateIDs, row.ID)
		}
	}
	result := db.Where("id IN ?", duplicateIDs).Delete(&models.EmailLog{})
	if result.Error != nil {
		return fmt.Errorf("删除邮件日志重复记录失败: keep=%s: %w", best.ID, result.Error)
	}
	if result.RowsAffected != int64(len(duplicateIDs)) {
		return fmt.Errorf(
			"删除邮件日志重复记录失败: keep=%s 预期删除=%d 实际删除=%d",
			best.ID,
			len(duplicateIDs),
			result.RowsAffected,
		)
	}
	return nil
}

func selectBestEmailLog(rows []models.EmailLog) models.EmailLog {
	best := rows[0]
	bestScore, bestTimestamp := emailLogQuality(best)
	for i := 1; i < len(rows); i++ {
		score, timestamp := emailLogQuality(rows[i])
		if score > bestScore ||
			(score == bestScore && timestamp > bestTimestamp) ||
			(score == bestScore && timestamp == bestTimestamp && rows[i].ID < best.ID) {
			best = rows[i]
			bestScore = score
			bestTimestamp = timestamp
		}
	}
	return best
}

func emailLogQuality(row models.EmailLog) (score int, createdAt int64) {
	switch strings.ToLower(strings.TrimSpace(row.Status)) {
	case "parsed":
		score += 200
	case "parsing":
		score += 80
	case "error":
		score += 20
	case "received":
		score += 10
	}
	if hasEmailLogText(row.ParsedInvoiceID) {
		score += 150
	}
	if hasEmailLogText(row.ParsedInvoiceIDs) {
		score += 150
	}
	if hasEmailLogText(row.InvoiceXMLURL) {
		score += 15
	}
	if hasEmailLogText(row.InvoicePDFURL) {
		score += 15
	}
	if row.HasAttachment != 0 {
		score += 5
	}
	if row.AttachmentCount > 0 {
		score += row.AttachmentCount
	}
	if hasEmailLogText(row.ReceivedDate) {
		score += 2
	}
	if hasEmailLogText(row.Subject) {
		score++
	}
	return score, row.CreatedAt.UnixNano()
}

func mergeEmailLogFields(best *models.EmailLog, rows []models.EmailLog) map[string]any {
	updates := make(map[string]any)
	for i := range rows {
		other := &rows[i]
		if other.ID == best.ID {
			continue
		}
		mergeEmailLogText(updates, "subject", &best.Subject, other.Subject)
		mergeEmailLogText(updates, "from_address", &best.FromAddress, other.FromAddress)
		mergeEmailLogText(updates, "received_date", &best.ReceivedDate, other.ReceivedDate)
		mergeEmailLogText(updates, "invoice_xml_url", &best.InvoiceXMLURL, other.InvoiceXMLURL)
		mergeEmailLogText(updates, "invoice_pdf_url", &best.InvoicePDFURL, other.InvoicePDFURL)
		mergeEmailLogText(updates, "parsed_invoice_id", &best.ParsedInvoiceID, other.ParsedInvoiceID)
		mergeEmailLogText(updates, "parsed_invoice_ids", &best.ParsedInvoiceIDs, other.ParsedInvoiceIDs)
		mergeEmailLogText(updates, "parse_error", &best.ParseError, other.ParseError)

		if best.HasAttachment < other.HasAttachment {
			best.HasAttachment = other.HasAttachment
			updates["has_attachment"] = other.HasAttachment
		}
		if best.AttachmentCount < other.AttachmentCount {
			best.AttachmentCount = other.AttachmentCount
			updates["attachment_count"] = other.AttachmentCount
		}
		if strings.TrimSpace(best.Status) == "" && strings.TrimSpace(other.Status) != "" {
			best.Status = strings.TrimSpace(other.Status)
			updates["status"] = best.Status
		}
	}
	return updates
}

func mergeEmailLogText(updates map[string]any, column string, current **string, candidate *string) {
	if hasEmailLogText(*current) || !hasEmailLogText(candidate) {
		return
	}
	value := strings.TrimSpace(*candidate)
	*current = &value
	updates[column] = value
}

func hasEmailLogText(value *string) bool {
	return value != nil && strings.TrimSpace(*value) != ""
}

func validateEmailLogIdentityIndex(db *gorm.DB) error {
	type indexRow struct {
		Name      string `gorm:"column:name"`
		IsUnique  int    `gorm:"column:unique"`
		IsPartial int    `gorm:"column:partial"`
	}
	var indexes []indexRow
	if err := db.Raw("PRAGMA index_list('email_logs')").Scan(&indexes).Error; err != nil {
		return fmt.Errorf("读取邮件日志索引失败: %w", err)
	}

	found := false
	for _, index := range indexes {
		if index.Name != emailLogIdentityIndex {
			continue
		}
		found = true
		if index.IsUnique != 1 || index.IsPartial != 0 {
			return fmt.Errorf("邮件日志身份索引 %s 不是完整唯一索引", emailLogIdentityIndex)
		}
		break
	}
	if !found {
		return fmt.Errorf("邮件日志身份索引 %s 未创建", emailLogIdentityIndex)
	}

	type indexColumn struct {
		Sequence int    `gorm:"column:seqno"`
		Name     string `gorm:"column:name"`
	}
	var columns []indexColumn
	if err := db.Raw("PRAGMA index_info('idx_email_logs_owner_cfg_box_uid')").Scan(&columns).Error; err != nil {
		return fmt.Errorf("读取邮件日志身份索引列失败: %w", err)
	}
	expected := []string{"owner_user_id", "email_config_id", "mailbox", "message_uid"}
	if len(columns) != len(expected) {
		return fmt.Errorf("邮件日志身份索引列数错误: 预期=%d 实际=%d", len(expected), len(columns))
	}
	for i, column := range columns {
		if column.Sequence != i || column.Name != expected[i] {
			return fmt.Errorf(
				"邮件日志身份索引第 %d 列错误: 预期=%s 实际=%s",
				i+1,
				expected[i],
				column.Name,
			)
		}
	}
	return nil
}
