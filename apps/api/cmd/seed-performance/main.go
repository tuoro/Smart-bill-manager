package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/sqlite"
)

const totalFacts = 10_000
const confirmationReviews = 220

type preparedStatements struct {
	document   *sql.Stmt
	page       *sql.Stmt
	job        *sql.Stmt
	aiRun      *sql.Stmt
	claim      *sql.Stmt
	field      *sql.Stmt
	evidence   *sql.Stmt
	validation *sql.Stmt
	decision   *sql.Stmt
	payment    *sql.Stmt
	invoice    *sql.Stmt
	item       *sql.Stmt
	origin     *sql.Stmt
	audit      *sql.Stmt
}

type seedContext struct {
	tx       *sql.Tx
	queries  preparedStatements
	tenantID string
	userID   string
	now      time.Time
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed-performance:", err)
		os.Exit(1)
	}
}

func run() error {
	var databasePath, migrationsDir, outputPath string
	flag.StringVar(&databasePath, "database", "", "fresh performance SQLite database")
	flag.StringVar(&migrationsDir, "migrations", "", "M1 migration directory")
	flag.StringVar(&outputPath, "output", "", "new seed manifest path")
	flag.Parse()
	if flag.NArg() != 0 || databasePath == "" || migrationsDir == "" || outputPath == "" {
		return errors.New("-database, -migrations, and -output are required; positional arguments are not allowed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	store, err := sqliteadapter.Open(ctx, sqliteadapter.Config{DatabasePath: databasePath, MigrationsDir: migrationsDir})
	if err != nil {
		return err
	}
	defer store.Close()
	tenantID, userID, err := performanceOwner(ctx, store.DB())
	if err != nil {
		return err
	}
	var existing int
	if err := store.DB().QueryRowContext(ctx, `
		SELECT (SELECT count(*) FROM payments) + (SELECT count(*) FROM invoices) + (SELECT count(*) FROM processing_jobs)
	`).Scan(&existing); err != nil {
		return fmt.Errorf("inspect performance database: %w", err)
	}
	if existing != 0 {
		return errors.New("performance seeding requires an owner-only database with no facts or jobs")
	}
	started := time.Now()
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin performance seed: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	providerID := makeID(0x01000001, 1)
	now := time.Date(2026, 8, 28, 2, 0, 0, 0, time.UTC)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO provider_configs (
			id, tenant_id, base_url, encrypted_api_key, model, output_mode, capability_status,
			capability_checked_at, capability_safe_message,
			capability_schema_version, capability_schema_sha256, active, version,
			safe_fingerprint, created_by_user_id, created_at, updated_at
		) VALUES (?, ?, 'https://performance.invalid/v1', ?, 'synthetic-performance-model', 'json_schema',
		          'passed', ?, 'synthetic performance seed', 'bill-visible-text-provider/1', ?, 1, 1,
		          'synthetic-performance-fingerprint', ?, ?, ?)
	`, providerID, tenantID, []byte{0x01, 0x02, 0x03}, formatTime(now), strings.Repeat("c", 64), userID, formatTime(now), formatTime(now)); err != nil {
		return fmt.Errorf("insert performance provider: %w", err)
	}
	queries, err := prepareSeedStatements(ctx, tx)
	if err != nil {
		return err
	}
	seed := seedContext{tx: tx, queries: queries, tenantID: tenantID, userID: userID, now: now}
	for index := 0; index < totalFacts; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := seed.insertChain(ctx, index, providerID); err != nil {
			return fmt.Errorf("seed chain %d: %w", index, err)
		}
	}
	confirmationJobIDs := make([]string, 0, confirmationReviews)
	for index := 0; index < confirmationReviews; index++ {
		jobID, err := seed.insertReadyPayment(ctx, totalFacts+index, providerID)
		if err != nil {
			return fmt.Errorf("seed confirmation review %d: %w", index, err)
		}
		confirmationJobIDs = append(confirmationJobIDs, jobID)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit performance seed: %w", err)
	}
	committed = true
	manifest := map[string]any{
		"seed_kind":                   "m1-performance-10k-facts",
		"payments":                    5_000,
		"invoices":                    5_000,
		"source_claim_chains":         totalFacts,
		"ready_confirmation_reviews":  confirmationReviews,
		"confirmation_job_ids":        confirmationJobIDs,
		"tenant_id":                   tenantID,
		"representative_document_id":  makeID(0x10000001, totalFacts-1),
		"representative_claim_set_id": makeID(0x40000001, totalFacts-1),
		"representative_job_id":       makeID(0x20000001, totalFacts-1),
		"seed_duration_ms":            time.Since(started).Milliseconds(),
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create performance seed manifest: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return fmt.Errorf("write performance seed manifest: %w", err)
	}
	return file.Close()
}

func performanceOwner(ctx context.Context, database *sql.DB) (string, string, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT tenant_id, user_id FROM memberships
		WHERE role = 'owner' AND status = 'active'
		ORDER BY tenant_id, user_id
	`)
	if err != nil {
		return "", "", fmt.Errorf("list performance owners: %w", err)
	}
	defer rows.Close()
	var tenantID, userID string
	count := 0
	for rows.Next() {
		if err := rows.Scan(&tenantID, &userID); err != nil {
			return "", "", err
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return "", "", err
	}
	if count != 1 {
		return "", "", fmt.Errorf("performance database must have exactly one active owner, found %d", count)
	}
	return tenantID, userID, nil
}

func prepareSeedStatements(ctx context.Context, tx *sql.Tx) (preparedStatements, error) {
	queries := []string{
		`INSERT INTO documents (id, tenant_id, storage_key, original_name, declared_mime, detected_mime, size_bytes, sha256, page_count, status, created_by_user_id, created_at) VALUES (?, ?, ?, ?, 'image/png', 'image/png', 1024, ?, 1, ?, ?, ?)`,
		`INSERT INTO document_pages (id, tenant_id, document_id, page_number, derived_image_storage_key, width, height, sha256, processing_version, created_at) VALUES (?, ?, ?, 1, ?, 1200, 800, ?, 'document-normalize/2', ?)`,
		`INSERT INTO processing_jobs (id, tenant_id, document_id, kind, status, attempt_count, created_at, version) VALUES (?, ?, ?, 'document_process', ?, 1, ?, 1)`,
		`INSERT INTO ai_runs (id, tenant_id, job_id, provider_config_id, provider_config_version, provider_config_fingerprint, model, prompt_version, extraction_schema_version, provider_schema_version, provider_schema_sha256, claim_schema_version, claim_mapper_version, input_processing_version, request_hash, response_hash, input_tokens, output_tokens, latency_ms, outcome, started_at, finished_at) VALUES (?, ?, ?, ?, 1, 'synthetic-performance-fingerprint', 'synthetic-performance-model', 'bill-visible-text-cn/1', 'bill-visible-text/1', 'bill-visible-text-provider/1', 'cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc', 'document-claim/2', 'claim-mapper/3', 'document-normalize/2', ?, ?, 10, 10, 1, 'succeeded', ?, ?)`,
		`INSERT INTO claim_sets (id, tenant_id, document_id, origin_ai_run_id, produced_by_ai_run_id, document_type, status, revision, optimistic_version, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, 1, 1, ?)`,
		`INSERT INTO field_claims (id, tenant_id, claim_set_id, field_path, value_type, presence, typed_value_json, normalized_value, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'ai', ?)`,
		`INSERT INTO evidence (id, tenant_id, field_claim_id, document_page_id, quote, evidence_hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		`INSERT INTO validation_results (id, tenant_id, claim_set_id, rule_code, severity, status, safe_message, rule_version, created_at) VALUES (?, ?, ?, 'claim_snapshot_complete', 'info', 'passed', 'synthetic performance claim', 'claim-validation/1', ?)`,
		`INSERT INTO review_decisions (id, tenant_id, claim_set_id, actor_user_id, action, association_mode, idempotency_key, expected_revision, created_at) VALUES (?, ?, ?, ?, 'confirm', 'no_candidate', ?, 1, ?)`,
		`INSERT INTO payments (id, tenant_id, source_review_decision_id, amount_minor, currency, merchant, transaction_time, source_timezone, created_at, updated_at, version) VALUES (?, ?, ?, ?, 'CNY', ?, ?, 'Asia/Shanghai', ?, ?, 1)`,
		`INSERT INTO invoices (id, tenant_id, source_review_decision_id, invoice_number, normalized_invoice_number, invoice_date, total_minor, currency, seller_name, buyer_name, created_at, updated_at, version) VALUES (?, ?, ?, ?, ?, ?, ?, 'CNY', ?, ?, ?, ?, 1)`,
		`INSERT INTO invoice_items (id, tenant_id, invoice_id, item_key, name, quantity, unit, unit_price_minor, amount_minor, sort_order) VALUES (?, ?, ?, ?, 'Synthetic performance item', '1', 'item', ?, ?, 0)`,
		`INSERT INTO fact_field_origins (id, tenant_id, payment_id, invoice_id, invoice_item_id, field_path, field_claim_id, review_decision_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		`INSERT INTO audit_events (id, tenant_id, actor_user_id, action, resource_type, resource_id, request_id, safe_metadata_json, created_at) VALUES (?, ?, ?, 'fact_confirmed', ?, ?, ?, '{}', ?)`,
	}
	prepared := make([]*sql.Stmt, 0, len(queries))
	for _, query := range queries {
		statement, err := tx.PrepareContext(ctx, query)
		if err != nil {
			for _, item := range prepared {
				_ = item.Close()
			}
			return preparedStatements{}, fmt.Errorf("prepare performance seed: %w", err)
		}
		prepared = append(prepared, statement)
	}
	return preparedStatements{
		document: prepared[0], page: prepared[1], job: prepared[2], aiRun: prepared[3],
		claim: prepared[4], field: prepared[5], evidence: prepared[6], validation: prepared[7],
		decision: prepared[8], payment: prepared[9], invoice: prepared[10], item: prepared[11],
		origin: prepared[12], audit: prepared[13],
	}, nil
}

func (s seedContext) insertChain(ctx context.Context, index int, providerID string) error {
	documentID := makeID(0x10000001, index)
	jobID := makeID(0x20000001, index)
	aiRunID := makeID(0x30000001, index)
	claimID := makeID(0x40000001, index)
	decisionID := makeID(0x50000001, index)
	factID := makeID(0x60000001, index)
	pageID := makeID(0x70000001, index)
	createdAt := s.now.Add(time.Duration(index) * time.Second)
	documentHash := hashString(fmt.Sprintf("performance-document-%d", index))
	if _, err := s.queries.document.ExecContext(ctx,
		documentID, s.tenantID, "tenants/"+s.tenantID+"/performance/"+documentID+"/original",
		fmt.Sprintf("synthetic-performance-%05d.png", index), documentHash, "completed", s.userID, formatTime(createdAt),
	); err != nil {
		return err
	}
	if _, err := s.queries.page.ExecContext(ctx,
		pageID, s.tenantID, documentID, "tenants/"+s.tenantID+"/performance/"+documentID+"/page-1.png",
		hashString(fmt.Sprintf("performance-page-%d", index)), formatTime(createdAt),
	); err != nil {
		return err
	}
	if _, err := s.queries.job.ExecContext(ctx, jobID, s.tenantID, documentID, "completed", formatTime(createdAt)); err != nil {
		return err
	}
	if _, err := s.queries.aiRun.ExecContext(ctx,
		aiRunID, s.tenantID, jobID, providerID, hashString("request-"+jobID), hashString("response-"+jobID),
		formatTime(createdAt), formatTime(createdAt.Add(time.Millisecond)),
	); err != nil {
		return err
	}
	documentType := "payment"
	if index >= totalFacts/2 {
		documentType = "invoice"
	}
	if _, err := s.queries.claim.ExecContext(ctx, claimID, s.tenantID, documentID, aiRunID, aiRunID, documentType, "confirmed", formatTime(createdAt)); err != nil {
		return err
	}
	fields, err := s.insertFields(ctx, index, claimID, pageID, documentType, createdAt)
	if err != nil {
		return err
	}
	if _, err := s.queries.validation.ExecContext(ctx, makeID(0x80000001, index), s.tenantID, claimID, formatTime(createdAt)); err != nil {
		return err
	}
	idempotencyKey := fmt.Sprintf("performance-confirm-%05d", index)
	if _, err := s.queries.decision.ExecContext(ctx, decisionID, s.tenantID, claimID, s.userID, idempotencyKey, formatTime(createdAt)); err != nil {
		return err
	}
	amount := int64(10_000 + index)
	if documentType == "payment" {
		merchant := fmt.Sprintf("Synthetic Merchant %05d", index)
		transactionTime := createdAt.Format(time.RFC3339)
		if _, err := s.queries.payment.ExecContext(ctx, factID, s.tenantID, decisionID, amount, merchant, transactionTime, formatTime(createdAt), formatTime(createdAt)); err != nil {
			return err
		}
		for _, path := range []string{"amount_minor", "currency", "merchant", "transaction_time", "source_timezone"} {
			if err := s.insertOrigin(ctx, index, path, fields[path], decisionID, factID, "", "", createdAt); err != nil {
				return err
			}
		}
	} else {
		invoiceNumber := fmt.Sprintf("PERF-INV-%05d", index)
		invoiceDate := createdAt.Format("2006-01-02")
		seller := fmt.Sprintf("Synthetic Seller %05d", index)
		buyer := fmt.Sprintf("Synthetic Buyer %05d", index)
		if _, err := s.queries.invoice.ExecContext(ctx,
			factID, s.tenantID, decisionID, invoiceNumber, strings.ToLower(invoiceNumber), invoiceDate,
			amount, seller, buyer, formatTime(createdAt), formatTime(createdAt),
		); err != nil {
			return err
		}
		itemID := makeID(0x90000001, index)
		itemKey := makeID(0xa0000001, index)
		if _, err := s.queries.item.ExecContext(ctx, itemID, s.tenantID, factID, itemKey, amount, amount); err != nil {
			return err
		}
		for _, path := range []string{"invoice_number", "invoice_date", "total_minor", "currency", "seller_name", "buyer_name"} {
			if err := s.insertOrigin(ctx, index, path, fields[path], decisionID, "", factID, "", createdAt); err != nil {
				return err
			}
		}
		itemPrefix := "items[" + itemKey + "]."
		for _, property := range []string{"name", "quantity", "unit", "unit_price_minor", "amount_minor", "sort_order"} {
			path := itemPrefix + property
			if err := s.insertOrigin(ctx, index, path, fields[path], decisionID, "", "", itemID, createdAt); err != nil {
				return err
			}
		}
	}
	resourceType := documentType
	if _, err := s.queries.audit.ExecContext(ctx,
		makeID(0xb0000001, index), s.tenantID, s.userID, resourceType, factID,
		fmt.Sprintf("performance-request-%05d", index), formatTime(createdAt),
	); err != nil {
		return err
	}
	return nil
}

func (s seedContext) insertReadyPayment(ctx context.Context, index int, providerID string) (string, error) {
	documentID := makeID(0x10000001, index)
	jobID := makeID(0x20000001, index)
	aiRunID := makeID(0x30000001, index)
	claimID := makeID(0x40000001, index)
	pageID := makeID(0x70000001, index)
	createdAt := s.now.Add(time.Duration(index) * time.Second)
	if _, err := s.queries.document.ExecContext(ctx,
		documentID, s.tenantID, "tenants/"+s.tenantID+"/performance/"+documentID+"/original",
		fmt.Sprintf("synthetic-confirm-%05d.png", index), hashString(fmt.Sprintf("performance-confirm-document-%d", index)),
		"needs_review", s.userID, formatTime(createdAt),
	); err != nil {
		return "", err
	}
	if _, err := s.queries.page.ExecContext(ctx,
		pageID, s.tenantID, documentID, "tenants/"+s.tenantID+"/performance/"+documentID+"/page-1.png",
		hashString(fmt.Sprintf("performance-confirm-page-%d", index)), formatTime(createdAt),
	); err != nil {
		return "", err
	}
	if _, err := s.queries.job.ExecContext(ctx, jobID, s.tenantID, documentID, "needs_review", formatTime(createdAt)); err != nil {
		return "", err
	}
	if _, err := s.queries.aiRun.ExecContext(ctx,
		aiRunID, s.tenantID, jobID, providerID, hashString("request-"+jobID), hashString("response-"+jobID),
		formatTime(createdAt), formatTime(createdAt.Add(time.Millisecond)),
	); err != nil {
		return "", err
	}
	if _, err := s.queries.claim.ExecContext(ctx,
		claimID, s.tenantID, documentID, aiRunID, aiRunID, "payment", "ready_for_review", formatTime(createdAt),
	); err != nil {
		return "", err
	}
	if _, err := s.insertFields(ctx, index, claimID, pageID, "payment", createdAt); err != nil {
		return "", err
	}
	if _, err := s.queries.validation.ExecContext(ctx, makeID(0x80000001, index), s.tenantID, claimID, formatTime(createdAt)); err != nil {
		return "", err
	}
	return jobID, nil
}

func (s seedContext) insertFields(
	ctx context.Context,
	index int,
	claimID, pageID, documentType string,
	createdAt time.Time,
) (map[string]string, error) {
	amount := int64(10_000 + index)
	values := []struct {
		path, valueType, presence string
		value, normalized         any
	}{
		{path: "document_type", valueType: "document_type", presence: "present", value: strconv.Quote(documentType), normalized: strconv.Quote(documentType)},
	}
	if documentType == "payment" {
		values = append(values,
			fieldValue("amount_minor", "money_minor", strconv.FormatInt(amount, 10), nil),
			fieldValue("currency", "string", `"CNY"`, nil),
			fieldValue("merchant", "string", strconv.Quote(fmt.Sprintf("Synthetic Merchant %05d", index)), strconv.Quote(fmt.Sprintf("synthetic merchant %05d", index))),
			fieldValue("transaction_time", "instant", strconv.Quote(createdAt.Format(time.RFC3339)), nil),
			fieldValue("source_timezone", "string", `"Asia/Shanghai"`, nil),
			absentField("payment_method", "string"), absentField("order_number", "string"), absentField("category", "string"),
		)
	} else {
		invoiceNumber := fmt.Sprintf("PERF-INV-%05d", index)
		itemKey := makeID(0xa0000001, index)
		prefix := "items[" + itemKey + "]."
		values = append(values,
			fieldValue("invoice_number", "string", strconv.Quote(invoiceNumber), strconv.Quote(strings.ToLower(invoiceNumber))),
			fieldValue("invoice_date", "date", strconv.Quote(createdAt.Format("2006-01-02")), nil),
			fieldValue("total_minor", "money_minor", strconv.FormatInt(amount, 10), nil),
			absentField("tax_minor", "money_minor"), fieldValue("currency", "string", `"CNY"`, nil),
			fieldValue("seller_name", "string", strconv.Quote(fmt.Sprintf("Synthetic Seller %05d", index)), strconv.Quote(fmt.Sprintf("synthetic seller %05d", index))),
			fieldValue("buyer_name", "string", strconv.Quote(fmt.Sprintf("Synthetic Buyer %05d", index)), strconv.Quote(fmt.Sprintf("synthetic buyer %05d", index))),
			fieldValue(prefix+"name", "string", `"Synthetic performance item"`, `"synthetic performance item"`),
			fieldValue(prefix+"quantity", "decimal", `"1"`, nil), fieldValue(prefix+"unit", "string", `"item"`, `"item"`),
			fieldValue(prefix+"unit_price_minor", "money_minor", strconv.FormatInt(amount, 10), nil),
			fieldValue(prefix+"amount_minor", "money_minor", strconv.FormatInt(amount, 10), nil), absentField(prefix+"tax_minor", "money_minor"),
			fieldValue(prefix+"sort_order", "integer", "0", nil),
		)
	}
	result := make(map[string]string, len(values))
	for fieldIndex, value := range values {
		fieldID := makeID(0xc0000001, index*32+fieldIndex)
		if _, err := s.queries.field.ExecContext(ctx,
			fieldID, s.tenantID, claimID, value.path, value.valueType, value.presence,
			value.value, value.normalized, formatTime(createdAt),
		); err != nil {
			return nil, err
		}
		result[value.path] = fieldID
		if value.presence == "present" && value.path != "document_type" {
			evidenceID := makeID(0xd0000001, index*32+fieldIndex)
			quote := "synthetic " + value.path
			if _, err := s.queries.evidence.ExecContext(ctx,
				evidenceID, s.tenantID, fieldID, pageID, quote, hashString(pageID+"\x00"+quote), formatTime(createdAt),
			); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}

func (s seedContext) insertOrigin(
	ctx context.Context,
	index int,
	path, fieldID, decisionID, paymentID, invoiceID, itemID string,
	createdAt time.Time,
) error {
	if fieldID == "" {
		return errors.New("missing field origin source for " + path)
	}
	originID := makeID(0xe0000001, index*32+originIndex(path))
	_, err := s.queries.origin.ExecContext(ctx,
		originID, s.tenantID, nullString(paymentID), nullString(invoiceID), nullString(itemID),
		path, fieldID, decisionID, formatTime(createdAt),
	)
	return err
}

func fieldValue(path, valueType string, value, normalized any) struct {
	path, valueType, presence string
	value, normalized         any
} {
	return struct {
		path, valueType, presence string
		value, normalized         any
	}{path: path, valueType: valueType, presence: "present", value: value, normalized: normalized}
}

func absentField(path, valueType string) struct {
	path, valueType, presence string
	value, normalized         any
} {
	return struct {
		path, valueType, presence string
		value, normalized         any
	}{path: path, valueType: valueType, presence: "absent"}
}

func originIndex(path string) int {
	indexes := map[string]int{
		"amount_minor": 1, "currency": 2, "merchant": 3, "transaction_time": 4, "source_timezone": 5,
		"invoice_number": 6, "invoice_date": 7, "total_minor": 8, "seller_name": 9, "buyer_name": 10,
	}
	if index, exists := indexes[path]; exists {
		return index
	}
	for index, suffix := range []string{"name", "quantity", "unit", "unit_price_minor", "amount_minor", "sort_order"} {
		if strings.HasPrefix(path, "items[") && strings.HasSuffix(path, "]."+suffix) {
			return 16 + index
		}
	}
	panic("unsupported performance origin path: " + path)
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func makeID(prefix uint32, sequence int) string {
	return fmt.Sprintf("%08x-0000-4000-8000-%012x", prefix, sequence)
}

func hashString(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
