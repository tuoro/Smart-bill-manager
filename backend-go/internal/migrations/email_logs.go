package migrations

import (
	"encoding/json"
	"fmt"
	"sort"
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
	updates, err := mergeEmailLogFields(&best, rows)
	if err != nil {
		return fmt.Errorf("合并邮件日志发票关联失败: keep=%s: %w", best.ID, err)
	}
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

func mergeEmailLogFields(best *models.EmailLog, rows []models.EmailLog) (map[string]any, error) {
	updates := make(map[string]any)
	if err := mergeParsedInvoiceAssociations(updates, best, rows); err != nil {
		return nil, err
	}

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
	return updates, nil
}

// mergeParsedInvoiceAssociations 将 scalar 和 JSON 列表中的全部发票 ID 合并为唯一的规范表示。
// primary 优先使用保留记录的 scalar；若其为空，则依次使用全体 scalar、全体 ID 的字典序最小值。
// 多 ID 列表始终把 primary 放在首位，其余 ID 按字典序排列，确保迁移结果与记录读取顺序无关。
func mergeParsedInvoiceAssociations(
	updates map[string]any,
	best *models.EmailLog,
	rows []models.EmailLog,
) error {
	allIDs := make(map[string]struct{})
	scalarIDs := make(map[string]struct{})
	for i := range rows {
		row := &rows[i]
		if scalar := normalizedEmailLogText(row.ParsedInvoiceID); scalar != "" {
			allIDs[scalar] = struct{}{}
			scalarIDs[scalar] = struct{}{}
		}

		listIDs, err := parseEmailLogInvoiceIDs(row.ParsedInvoiceIDs)
		if err != nil {
			return fmt.Errorf("邮件日志 %s 的 parsed_invoice_ids 无效: %w", row.ID, err)
		}
		for _, id := range listIDs {
			allIDs[id] = struct{}{}
		}
	}

	if len(allIDs) == 0 {
		setEmailLogText(updates, "parsed_invoice_id", &best.ParsedInvoiceID, nil)
		setEmailLogText(updates, "parsed_invoice_ids", &best.ParsedInvoiceIDs, nil)
		return nil
	}

	primary := normalizedEmailLogText(best.ParsedInvoiceID)
	if primary == "" && len(scalarIDs) > 0 {
		primary = sortedEmailLogIDs(scalarIDs)[0]
	}
	if primary == "" {
		primary = sortedEmailLogIDs(allIDs)[0]
	}

	orderedIDs := []string{primary}
	remainingIDs := make(map[string]struct{}, len(allIDs)-1)
	for id := range allIDs {
		if id != primary {
			remainingIDs[id] = struct{}{}
		}
	}
	orderedIDs = append(orderedIDs, sortedEmailLogIDs(remainingIDs)...)

	primaryValue := primary
	setEmailLogText(updates, "parsed_invoice_id", &best.ParsedInvoiceID, &primaryValue)
	if len(orderedIDs) == 1 {
		setEmailLogText(updates, "parsed_invoice_ids", &best.ParsedInvoiceIDs, nil)
		return nil
	}

	encoded, err := json.Marshal(orderedIDs)
	if err != nil {
		return fmt.Errorf("序列化 parsed_invoice_ids 失败: %w", err)
	}
	encodedValue := string(encoded)
	setEmailLogText(updates, "parsed_invoice_ids", &best.ParsedInvoiceIDs, &encodedValue)
	return nil
}

func parseEmailLogInvoiceIDs(value *string) ([]string, error) {
	if !hasEmailLogText(value) {
		return nil, nil
	}

	raw := strings.TrimSpace(*value)
	if !strings.HasPrefix(raw, "[") {
		return nil, fmt.Errorf("必须是 JSON 字符串数组")
	}
	var encodedIDs []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &encodedIDs); err != nil {
		return nil, fmt.Errorf("解析 JSON 数组失败: %w", err)
	}
	if encodedIDs == nil || len(encodedIDs) == 0 {
		return nil, fmt.Errorf("JSON 数组不能为空")
	}

	ids := make([]string, 0, len(encodedIDs))
	for i, encodedID := range encodedIDs {
		var id string
		if err := json.Unmarshal(encodedID, &id); err != nil {
			return nil, fmt.Errorf("第 %d 项必须是字符串: %w", i+1, err)
		}
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("第 %d 项不能为空", i+1)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func sortedEmailLogIDs(ids map[string]struct{}) []string {
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func normalizedEmailLogText(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func setEmailLogText(updates map[string]any, column string, current **string, desired *string) {
	if desired == nil {
		if *current != nil {
			*current = nil
			updates[column] = nil
		}
		return
	}
	if *current != nil && **current == *desired {
		return
	}
	value := *desired
	*current = &value
	updates[column] = value
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
