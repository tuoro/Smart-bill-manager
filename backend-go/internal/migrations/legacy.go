package migrations

import (
	"fmt"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/utils"

	"gorm.io/gorm"
)

func migrateLegacyDataAndIndexes(db *gorm.DB) error {
	steps := []func(*gorm.DB) error{
		migrateLegacyTripAssignmentColumns,
		backfillNumericTimestamps,
		backfillOwners,
		createIndexes,
		backfillInvoiceDates,
		backfillOCRBlobs,
	}
	for _, step := range steps {
		if err := step(db); err != nil {
			return err
		}
	}
	return nil
}

func migrateLegacyTripAssignmentColumns(db *gorm.DB) error {
	if db.Migrator().HasColumn("payments", "trip_assign_src") {
		if err := execSQL(db, "回填旧行程分配来源", `
			UPDATE payments
			SET trip_assignment_source = COALESCE(NULLIF(TRIM(trip_assignment_source), ''), trip_assign_src, 'auto')
		`); err != nil {
			return err
		}
	}
	if db.Migrator().HasColumn("payments", "trip_assign_state") {
		if err := execSQL(db, "回填旧行程分配状态", `
			UPDATE payments
			SET trip_assignment_state = COALESCE(NULLIF(TRIM(trip_assignment_state), ''), trip_assign_state, 'no_match')
		`); err != nil {
			return err
		}
	}
	return nil
}

func backfillNumericTimestamps(db *gorm.DB) error {
	queries := []struct {
		name string
		sql  string
	}{
		{
			name: "回填支付时间戳",
			sql: `UPDATE payments
				SET transaction_time_ts = CAST(strftime('%s', transaction_time) AS INTEGER) * 1000
				WHERE transaction_time_ts = 0
				  AND transaction_time IS NOT NULL
				  AND TRIM(transaction_time) != ''
				  AND strftime('%s', transaction_time) IS NOT NULL`,
		},
		{
			name: "回填行程开始时间戳",
			sql: `UPDATE trips
				SET start_time_ts = CAST(strftime('%s', start_time) AS INTEGER) * 1000
				WHERE start_time_ts = 0
				  AND start_time IS NOT NULL
				  AND TRIM(start_time) != ''
				  AND strftime('%s', start_time) IS NOT NULL`,
		},
		{
			name: "回填行程结束时间戳",
			sql: `UPDATE trips
				SET end_time_ts = CAST(strftime('%s', end_time) AS INTEGER) * 1000
				WHERE end_time_ts = 0
				  AND end_time IS NOT NULL
				  AND TRIM(end_time) != ''
				  AND strftime('%s', end_time) IS NOT NULL`,
		},
	}
	for _, query := range queries {
		if err := execSQL(db, query.name, query.sql); err != nil {
			return err
		}
	}
	return nil
}

func backfillOwners(db *gorm.DB) error {
	var firstUser models.User
	if err := db.Select("id").Order("created_at ASC").First(&firstUser).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return fmt.Errorf("查找旧数据默认所有者失败: %w", err)
	}
	ownerID := strings.TrimSpace(firstUser.ID)
	if ownerID == "" {
		return nil
	}

	queries := []struct {
		name string
		sql  string
		args []any
	}{
		{name: "回填支付所有者", sql: `UPDATE payments SET owner_user_id = ? WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`, args: []any{ownerID}},
		{name: "回填行程所有者", sql: `UPDATE trips SET owner_user_id = ? WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`, args: []any{ownerID}},
		{name: "回填发票所有者", sql: `UPDATE invoices SET owner_user_id = ? WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`, args: []any{ownerID}},
		{name: "回填邮箱配置所有者", sql: `UPDATE email_configs SET owner_user_id = ? WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`, args: []any{ownerID}},
		{
			name: "回填发票附件所有者",
			sql: `UPDATE invoice_attachments
				SET owner_user_id = COALESCE(
					NULLIF(TRIM((SELECT owner_user_id FROM invoices WHERE invoices.id = invoice_attachments.invoice_id)), ''),
					?
				)
				WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`,
			args: []any{ownerID},
		},
		{
			name: "回填发票 OCR 所有者",
			sql: `UPDATE invoice_ocr_blobs
				SET owner_user_id = COALESCE(
					NULLIF(TRIM((SELECT owner_user_id FROM invoices WHERE invoices.id = invoice_ocr_blobs.invoice_id)), ''),
					?
				)
				WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`,
			args: []any{ownerID},
		},
		{
			name: "回填支付 OCR 所有者",
			sql: `UPDATE payment_ocr_blobs
				SET owner_user_id = COALESCE(
					NULLIF(TRIM((SELECT owner_user_id FROM payments WHERE payments.id = payment_ocr_blobs.payment_id)), ''),
					?
				)
				WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`,
			args: []any{ownerID},
		},
		{
			name: "回填邮件日志所有者",
			sql: `UPDATE email_logs
				SET owner_user_id = COALESCE(
					NULLIF(TRIM((SELECT owner_user_id FROM email_configs WHERE email_configs.id = email_logs.email_config_id)), ''),
					?
				)
				WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`,
			args: []any{ownerID},
		},
		{
			name: "回填任务所有者",
			sql: `UPDATE tasks
				SET owner_user_id = COALESCE(NULLIF(TRIM(created_by), ''), ?)
				WHERE owner_user_id IS NULL OR TRIM(owner_user_id) = ''`,
			args: []any{ownerID},
		},
	}
	for _, query := range queries {
		if err := execSQL(db, query.name, query.sql, query.args...); err != nil {
			return err
		}
	}
	return nil
}

func createIndexes(db *gorm.DB) error {
	statements := []string{
		"CREATE INDEX IF NOT EXISTS idx_users_username ON users(username)",
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_invites_code_hash ON invites(code_hash)",
		"CREATE INDEX IF NOT EXISTS idx_invites_created_at ON invites(created_at)",
		"CREATE INDEX IF NOT EXISTS idx_invites_used_at ON invites(used_at)",
		"CREATE INDEX IF NOT EXISTS idx_payments_time ON payments(transaction_time)",
		"CREATE INDEX IF NOT EXISTS idx_payments_time_ts ON payments(transaction_time_ts)",
		"CREATE INDEX IF NOT EXISTS idx_payments_owner_draft_time_ts ON payments(owner_user_id, is_draft, transaction_time_ts)",
		"CREATE INDEX IF NOT EXISTS idx_payments_owner_draft_category_time_ts ON payments(owner_user_id, is_draft, category, transaction_time_ts)",
		"CREATE INDEX IF NOT EXISTS idx_payments_trip_id ON payments(trip_id)",
		"CREATE INDEX IF NOT EXISTS idx_payments_bad_debt ON payments(bad_debt)",
		"CREATE INDEX IF NOT EXISTS idx_payments_trip_assign_src ON payments(trip_assignment_source)",
		"CREATE INDEX IF NOT EXISTS idx_payments_trip_assign_state ON payments(trip_assignment_state)",
		"CREATE INDEX IF NOT EXISTS idx_payments_file_sha256 ON payments(file_sha256)",
		"CREATE INDEX IF NOT EXISTS idx_payments_dedup_status ON payments(dedup_status)",
		"CREATE INDEX IF NOT EXISTS idx_trips_time ON trips(start_time, end_time)",
		"CREATE INDEX IF NOT EXISTS idx_trips_time_ts ON trips(start_time_ts, end_time_ts)",
		"CREATE INDEX IF NOT EXISTS idx_trips_timezone ON trips(timezone)",
		"CREATE INDEX IF NOT EXISTS idx_trips_reimburse_status ON trips(reimburse_status)",
		"CREATE INDEX IF NOT EXISTS idx_trips_bad_debt_locked ON trips(bad_debt_locked)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_date ON invoices(invoice_date)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_date_ymd ON invoices(invoice_date_ymd)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_owner_draft_created_at ON invoices(owner_user_id, is_draft, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_owner_draft_date_ymd ON invoices(owner_user_id, is_draft, invoice_date_ymd)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_invoice_number ON invoices(invoice_number)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_owner_invoice_number ON invoices(owner_user_id, invoice_number)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_bad_debt ON invoices(bad_debt)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_file_sha256 ON invoices(file_sha256)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_owner_file_sha256 ON invoices(owner_user_id, file_sha256)",
		"CREATE INDEX IF NOT EXISTS idx_invoices_dedup_status ON invoices(dedup_status)",
		"CREATE INDEX IF NOT EXISTS idx_invoice_ocr_blobs_owner_invoice ON invoice_ocr_blobs(owner_user_id, invoice_id)",
		"CREATE INDEX IF NOT EXISTS idx_payment_ocr_blobs_owner_payment ON payment_ocr_blobs(owner_user_id, payment_id)",
		"CREATE INDEX IF NOT EXISTS idx_tasks_status_created_at ON tasks(status, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_tasks_created_by ON tasks(created_by)",
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_regression_samples_source ON regression_samples(source_type, source_id, kind)",
		"CREATE INDEX IF NOT EXISTS idx_regression_samples_kind_created_at ON regression_samples(kind, created_at)",
		"CREATE INDEX IF NOT EXISTS idx_regression_samples_name ON regression_samples(name)",
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_regression_samples_kind_rawhash ON regression_samples(kind, raw_hash) WHERE raw_hash != ''",
		"CREATE UNIQUE INDEX IF NOT EXISTS ux_invoice_payment_links_invoice_id ON invoice_payment_links(invoice_id)",
		"DROP INDEX IF EXISTS ux_invoice_payment_links_payment_id",
		"CREATE INDEX IF NOT EXISTS idx_invoice_payment_links_payment_id ON invoice_payment_links(payment_id)",
		"CREATE INDEX IF NOT EXISTS idx_email_logs_date ON email_logs(created_at)",
	}
	for _, statement := range statements {
		if err := execSQL(db, "创建数据库索引", statement); err != nil {
			return err
		}
	}
	return nil
}

func backfillInvoiceDates(db *gorm.DB) error {
	queries := []string{
		`UPDATE invoices
		 SET invoice_date_ymd = SUBSTR(invoice_date, 1, 10)
		 WHERE (invoice_date_ymd IS NULL OR TRIM(invoice_date_ymd) = '')
		   AND invoice_date IS NOT NULL
		   AND invoice_date LIKE '____-__-__%'`,
		`UPDATE invoices
		 SET invoice_date_ymd = REPLACE(SUBSTR(invoice_date, 1, 10), '/', '-')
		 WHERE (invoice_date_ymd IS NULL OR TRIM(invoice_date_ymd) = '')
		   AND invoice_date IS NOT NULL
		   AND invoice_date LIKE '____/__/__%'`,
	}
	for _, query := range queries {
		if err := execSQL(db, "回填标准发票日期", query); err != nil {
			return err
		}
	}

	type invoiceDateRow struct {
		ID          string  `gorm:"column:id"`
		InvoiceDate *string `gorm:"column:invoice_date"`
	}
	var missing []invoiceDateRow
	if err := db.Raw(`
		SELECT id, invoice_date
		FROM invoices
		WHERE invoice_date IS NOT NULL
		  AND TRIM(invoice_date) != ''
		  AND (invoice_date_ymd IS NULL OR TRIM(invoice_date_ymd) = '')
	`).Scan(&missing).Error; err != nil {
		return fmt.Errorf("读取待规范化发票日期失败: %w", err)
	}
	for _, row := range missing {
		if row.ID == "" || row.InvoiceDate == nil {
			continue
		}
		ymd := utils.NormalizeDateYMD(*row.InvoiceDate)
		if ymd == "" {
			continue
		}
		if err := execSQL(db, "更新规范化发票日期", `UPDATE invoices SET invoice_date_ymd = ? WHERE id = ?`, ymd, row.ID); err != nil {
			return err
		}
	}
	return nil
}

func backfillOCRBlobs(db *gorm.DB) error {
	// 外层括号保证 NOT EXISTS 同时约束 extracted_data 和 raw_text 两个分支。
	if err := execSQL(db, "回填发票 OCR 大字段", `
		INSERT INTO invoice_ocr_blobs (invoice_id, owner_user_id, extracted_data, raw_text, created_at, updated_at)
		SELECT id, owner_user_id, extracted_data, raw_text, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM invoices
		WHERE (
			(extracted_data IS NOT NULL AND TRIM(extracted_data) != '')
			OR (raw_text IS NOT NULL AND TRIM(raw_text) != '')
		)
		AND NOT EXISTS (
			SELECT 1 FROM invoice_ocr_blobs b WHERE b.invoice_id = invoices.id
		)
	`); err != nil {
		return err
	}
	return execSQL(db, "回填支付 OCR 大字段", `
		INSERT INTO payment_ocr_blobs (payment_id, owner_user_id, extracted_data, created_at, updated_at)
		SELECT id, owner_user_id, extracted_data, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM payments
		WHERE extracted_data IS NOT NULL
		  AND TRIM(extracted_data) != ''
		  AND NOT EXISTS (
			SELECT 1 FROM payment_ocr_blobs b WHERE b.payment_id = payments.id
		  )
	`)
}

func execSQL(db *gorm.DB, operation, query string, args ...any) error {
	if err := db.Exec(query, args...).Error; err != nil {
		return fmt.Errorf("%s失败: %w", operation, err)
	}
	return nil
}
