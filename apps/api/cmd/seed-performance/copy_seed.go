package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

type generatedCopyRows struct {
	count  int
	index  int
	values func(int) ([]any, error)
}

func (rows *generatedCopyRows) Next() bool {
	rows.index++
	return rows.index < rows.count
}

func (rows *generatedCopyRows) Values() ([]any, error) {
	return rows.values(rows.index)
}

func (rows *generatedCopyRows) Err() error { return nil }

func copyGenerated(
	ctx context.Context,
	tx pgx.Tx,
	table string,
	columns []string,
	count int,
	values func(int) ([]any, error),
) error {
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{table}, columns, &generatedCopyRows{count: count, index: -1, values: values})
	if err != nil {
		return fmt.Errorf("copy performance %s: %w", table, err)
	}
	if copied != int64(count) {
		return fmt.Errorf("copy performance %s: expected %d rows, copied %d", table, count, copied)
	}
	return nil
}

func seedPerformanceData(ctx context.Context, tx pgx.Tx, tenantID, userID, providerID string, now time.Time) error {
	const totalDocuments = totalFacts + confirmationReviews
	if err := copyGenerated(ctx, tx, "documents", []string{
		"id", "tenant_id", "storage_key", "original_name", "declared_mime", "detected_mime",
		"size_bytes", "sha256", "page_count", "status", "created_by_user_id", "created_at",
	}, totalDocuments, func(index int) ([]any, error) {
		id := makeID(0x10000001, index)
		status, kind := "completed", "performance"
		if index >= totalFacts {
			status, kind = "needs_review", "confirm"
		}
		return []any{
			id, tenantID, "tenants/" + tenantID + "/performance/" + id + "/original",
			fmt.Sprintf("synthetic-%s-%05d.png", kind, index), "image/png", "image/png", int64(1024),
			hashString(fmt.Sprintf("performance-%s-document-%d", kind, index)), int64(1), status,
			userID, now.Add(time.Duration(index) * time.Second),
		}, nil
	}); err != nil {
		return err
	}
	if err := copyGenerated(ctx, tx, "document_pages", []string{
		"id", "tenant_id", "document_id", "page_number", "derived_image_storage_key", "width", "height",
		"sha256", "processing_version", "visual_fingerprint_version", "dhash64", "ahash64",
		"dhash_band_0", "dhash_band_1", "dhash_band_2", "dhash_band_3", "created_at",
	}, totalDocuments, func(index int) ([]any, error) {
		documentID := makeID(0x10000001, index)
		pageID := makeID(0x70000001, index)
		kind := "page"
		if index >= totalFacts {
			kind = "confirm-page"
		}
		dhash, ahash, bands := syntheticPageFingerprint(fmt.Sprintf("performance-%s-%d", kind, index))
		return []any{
			pageID, tenantID, documentID, int64(1), "tenants/" + tenantID + "/performance/" + documentID + "/page-1.png",
			int64(1200), int64(800), hashString(fmt.Sprintf("performance-%s-%d", kind, index)),
			"document-normalize/2", "page-visual-dedup/1", dhash, ahash,
			int64(bands[0]), int64(bands[1]), int64(bands[2]), int64(bands[3]),
			now.Add(time.Duration(index) * time.Second),
		}, nil
	}); err != nil {
		return err
	}
	if err := copyGenerated(ctx, tx, "processing_jobs", []string{
		"id", "tenant_id", "document_id", "kind", "status", "attempt_count", "created_at", "version",
	}, totalDocuments, func(index int) ([]any, error) {
		status := "completed"
		if index >= totalFacts {
			status = "needs_review"
		}
		return []any{makeID(0x20000001, index), tenantID, makeID(0x10000001, index), "document_process", status,
			int64(1), now.Add(time.Duration(index) * time.Second), int64(1)}, nil
	}); err != nil {
		return err
	}
	if err := copyGenerated(ctx, tx, "ai_runs", []string{
		"id", "tenant_id", "job_id", "provider_config_id", "provider_config_version", "provider_config_fingerprint",
		"model", "prompt_version", "extraction_schema_version", "provider_schema_version", "provider_schema_sha256",
		"claim_schema_version", "claim_mapper_version", "input_processing_version", "request_hash", "response_hash",
		"input_tokens", "output_tokens", "latency_ms", "outcome", "started_at", "finished_at",
	}, totalDocuments, func(index int) ([]any, error) {
		jobID := makeID(0x20000001, index)
		createdAt := now.Add(time.Duration(index) * time.Second)
		return []any{
			makeID(0x30000001, index), tenantID, jobID, providerID, int64(1), "synthetic-performance-fingerprint",
			"synthetic-performance-model", "bill-visible-text-cn/2", "bill-visible-text/2", "bill-visible-text-provider/2",
			strings.Repeat("c", 64), "document-claim/3", "claim-mapper/4", "document-normalize/2",
			hashString("request-" + jobID), hashString("response-" + jobID), int64(10), int64(10), int64(1), "succeeded",
			createdAt, createdAt.Add(time.Millisecond),
		}, nil
	}); err != nil {
		return err
	}
	if err := copyGenerated(ctx, tx, "claim_sets", []string{
		"id", "tenant_id", "document_id", "origin_ai_run_id", "produced_by_ai_run_id", "document_type",
		"status", "revision", "optimistic_version", "created_at",
	}, totalDocuments, func(index int) ([]any, error) {
		documentType, status := "payment", "confirmed"
		if index >= totalFacts/2 && index < totalFacts {
			documentType = "invoice"
		}
		if index >= totalFacts {
			status = "ready_for_review"
		}
		aiRunID := makeID(0x30000001, index)
		return []any{makeID(0x40000001, index), tenantID, makeID(0x10000001, index), aiRunID, aiRunID,
			documentType, status, int64(1), int64(1), now.Add(time.Duration(index) * time.Second)}, nil
	}); err != nil {
		return err
	}

	if err := copyFieldRange(ctx, tx, tenantID, now, 0, totalFacts/2, "payment"); err != nil {
		return err
	}
	if err := copyFieldRange(ctx, tx, tenantID, now, totalFacts/2, totalFacts/2, "invoice"); err != nil {
		return err
	}
	if err := copyFieldRange(ctx, tx, tenantID, now, totalFacts, confirmationReviews, "payment"); err != nil {
		return err
	}
	if err := copyEvidenceRange(ctx, tx, tenantID, now, 0, totalFacts/2, "payment"); err != nil {
		return err
	}
	if err := copyEvidenceRange(ctx, tx, tenantID, now, totalFacts/2, totalFacts/2, "invoice"); err != nil {
		return err
	}
	if err := copyEvidenceRange(ctx, tx, tenantID, now, totalFacts, confirmationReviews, "payment"); err != nil {
		return err
	}
	if err := copyGenerated(ctx, tx, "validation_results", []string{
		"id", "tenant_id", "claim_set_id", "rule_code", "severity", "status", "safe_message", "rule_version", "created_at",
	}, totalDocuments, func(index int) ([]any, error) {
		return []any{makeID(0x80000001, index), tenantID, makeID(0x40000001, index), "claim_snapshot_complete",
			"info", "passed", "synthetic performance claim", "claim-validation/1",
			now.Add(time.Duration(index) * time.Second)}, nil
	}); err != nil {
		return err
	}
	if err := copyGenerated(ctx, tx, "review_decisions", []string{
		"id", "tenant_id", "claim_set_id", "actor_user_id", "action", "association_mode", "duplicate_plan_hash",
		"idempotency_key", "expected_revision", "created_at",
	}, totalFacts, func(index int) ([]any, error) {
		return []any{makeID(0x50000001, index), tenantID, makeID(0x40000001, index), userID, "confirm", "no_candidate",
			"4f53cda18c2baa0c0354bb5f9a3ecbe5ed12ab4d8e11ba873c2f11161202b945",
			fmt.Sprintf("performance-confirm-%05d", index), int64(1), now.Add(time.Duration(index) * time.Second)}, nil
	}); err != nil {
		return err
	}
	if err := copyGenerated(ctx, tx, "payments", []string{
		"id", "tenant_id", "source_review_decision_id", "amount_minor", "currency", "merchant", "transaction_time",
		"source_timezone", "business_date", "created_at", "updated_at", "version",
	}, totalFacts/2, func(index int) ([]any, error) {
		createdAt := now.Add(time.Duration(index) * time.Second)
		transactionTime := createdAt.Format(time.RFC3339)
		businessDate, err := domain.PaymentBusinessDate(transactionTime, "Asia/Shanghai")
		if err != nil {
			return nil, err
		}
		businessDay, err := time.Parse("2006-01-02", businessDate)
		if err != nil {
			return nil, err
		}
		return []any{makeID(0x60000001, index), tenantID, makeID(0x50000001, index), int64(10_000 + index), "CNY",
			fmt.Sprintf("Synthetic Merchant %05d", index), createdAt, "Asia/Shanghai", businessDay,
			createdAt, createdAt, int64(1)}, nil
	}); err != nil {
		return err
	}
	if err := copyGenerated(ctx, tx, "invoices", []string{
		"id", "tenant_id", "source_review_decision_id", "invoice_number", "normalized_invoice_number", "invoice_date",
		"total_minor", "currency", "seller_name", "buyer_name", "created_at", "updated_at", "version",
	}, totalFacts/2, func(offset int) ([]any, error) {
		index := totalFacts/2 + offset
		createdAt := now.Add(time.Duration(index) * time.Second)
		invoiceNumber := fmt.Sprintf("PERF-INV-%05d", index)
		return []any{makeID(0x60000001, index), tenantID, makeID(0x50000001, index), invoiceNumber,
			strings.ToLower(invoiceNumber), createdAt, int64(10_000 + index), "CNY",
			fmt.Sprintf("Synthetic Seller %05d", index), fmt.Sprintf("Synthetic Buyer %05d", index),
			createdAt, createdAt, int64(1)}, nil
	}); err != nil {
		return err
	}
	if err := copyGenerated(ctx, tx, "invoice_items", []string{
		"id", "tenant_id", "invoice_id", "item_key", "name", "quantity", "unit", "unit_price_minor", "amount_minor", "sort_order",
	}, totalFacts/2, func(offset int) ([]any, error) {
		index := totalFacts/2 + offset
		amount := int64(10_000 + index)
		return []any{makeID(0x90000001, index), tenantID, makeID(0x60000001, index), makeID(0xa0000001, index),
			"Synthetic performance item", "1", "item", amount, amount, int64(0)}, nil
	}); err != nil {
		return err
	}
	if err := copyOriginRange(ctx, tx, tenantID, now, 0, totalFacts/2, "payment"); err != nil {
		return err
	}
	if err := copyOriginRange(ctx, tx, tenantID, now, totalFacts/2, totalFacts/2, "invoice"); err != nil {
		return err
	}
	return copyGenerated(ctx, tx, "audit_events", []string{
		"id", "tenant_id", "actor_user_id", "action", "resource_type", "resource_id", "request_id", "safe_metadata_json", "created_at",
	}, totalFacts, func(index int) ([]any, error) {
		resourceType := "payment"
		if index >= totalFacts/2 {
			resourceType = "invoice"
		}
		return []any{makeID(0xb0000001, index), tenantID, userID, "fact_confirmed", resourceType,
			makeID(0x60000001, index), fmt.Sprintf("performance-request-%05d", index), "{}",
			now.Add(time.Duration(index) * time.Second)}, nil
	})
}

func performanceFieldValues(index int, documentType string, createdAt time.Time) []performanceFieldValue {
	amount := int64(10_000 + index)
	values := []performanceFieldValue{{
		path: "document_type", valueType: "document_type", presence: "present",
		value: strconv.Quote(documentType), normalized: strconv.Quote(documentType),
	}}
	if documentType == "payment" {
		return append(values,
			fieldValue("amount_minor", "money_minor", strconv.FormatInt(amount, 10), nil),
			fieldValue("currency", "string", `"CNY"`, nil),
			fieldValue("merchant", "string", strconv.Quote(fmt.Sprintf("Synthetic Merchant %05d", index)), strconv.Quote(fmt.Sprintf("synthetic merchant %05d", index))),
			fieldValue("transaction_time", "instant", strconv.Quote(createdAt.Format(time.RFC3339)), nil),
			fieldValue("source_timezone", "string", `"Asia/Shanghai"`, nil),
			absentField("payment_method", "string"), absentField("order_number", "string"), absentField("category", "string"),
		)
	}
	invoiceNumber := fmt.Sprintf("PERF-INV-%05d", index)
	itemKey := makeID(0xa0000001, index)
	prefix := "items[" + itemKey + "]."
	return append(values,
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

func copyFieldRange(ctx context.Context, tx pgx.Tx, tenantID string, now time.Time, start, documents int, documentType string) error {
	perDocument := len(performanceFieldValues(start, documentType, now.Add(time.Duration(start)*time.Second)))
	return copyGenerated(ctx, tx, "field_claims", []string{
		"id", "tenant_id", "claim_set_id", "field_path", "value_type", "presence", "typed_value_json", "normalized_value", "source", "created_at",
	}, documents*perDocument, func(flat int) ([]any, error) {
		index := start + flat/perDocument
		fieldIndex := flat % perDocument
		createdAt := now.Add(time.Duration(index) * time.Second)
		value := performanceFieldValues(index, documentType, createdAt)[fieldIndex]
		return []any{makeID(0xc0000001, index*32+fieldIndex), tenantID, makeID(0x40000001, index), value.path,
			value.valueType, value.presence, value.value, value.normalized, "ai", createdAt}, nil
	})
}

func presentEvidenceFields(index int, documentType string, createdAt time.Time) []struct {
	fieldIndex int
	value      performanceFieldValue
} {
	result := make([]struct {
		fieldIndex int
		value      performanceFieldValue
	}, 0, 12)
	for fieldIndex, value := range performanceFieldValues(index, documentType, createdAt) {
		if value.presence == "present" && value.path != "document_type" {
			result = append(result, struct {
				fieldIndex int
				value      performanceFieldValue
			}{fieldIndex: fieldIndex, value: value})
		}
	}
	return result
}

func copyEvidenceRange(ctx context.Context, tx pgx.Tx, tenantID string, now time.Time, start, documents int, documentType string) error {
	perDocument := len(presentEvidenceFields(start, documentType, now.Add(time.Duration(start)*time.Second)))
	return copyGenerated(ctx, tx, "evidence", []string{
		"id", "tenant_id", "field_claim_id", "document_page_id", "quote", "evidence_hash", "created_at",
	}, documents*perDocument, func(flat int) ([]any, error) {
		index := start + flat/perDocument
		createdAt := now.Add(time.Duration(index) * time.Second)
		field := presentEvidenceFields(index, documentType, createdAt)[flat%perDocument]
		pageID := makeID(0x70000001, index)
		quote := "synthetic " + field.value.path
		return []any{makeID(0xd0000001, index*32+field.fieldIndex), tenantID,
			makeID(0xc0000001, index*32+field.fieldIndex), pageID, quote,
			hashString(pageID + "\x00" + quote), createdAt}, nil
	})
}

func copyOriginRange(ctx context.Context, tx pgx.Tx, tenantID string, now time.Time, start, documents int, documentType string) error {
	paths := []string{"amount_minor", "currency", "merchant", "transaction_time", "source_timezone"}
	if documentType == "invoice" {
		paths = []string{"invoice_number", "invoice_date", "total_minor", "currency", "seller_name", "buyer_name", "name", "quantity", "unit", "unit_price_minor", "amount_minor", "sort_order"}
	}
	return copyGenerated(ctx, tx, "fact_field_origins", []string{
		"id", "tenant_id", "payment_id", "invoice_id", "invoice_item_id", "field_path", "field_claim_id", "review_decision_id", "created_at",
	}, documents*len(paths), func(flat int) ([]any, error) {
		index := start + flat/len(paths)
		path := paths[flat%len(paths)]
		createdAt := now.Add(time.Duration(index) * time.Second)
		fieldPath := path
		paymentID, invoiceID, itemID := "", "", ""
		if documentType == "payment" {
			paymentID = makeID(0x60000001, index)
		} else if flat%len(paths) < 6 {
			invoiceID = makeID(0x60000001, index)
		} else {
			itemID = makeID(0x90000001, index)
			fieldPath = "items[" + makeID(0xa0000001, index) + "]." + path
		}
		fields := performanceFieldValues(index, documentType, createdAt)
		fieldID := ""
		for fieldIndex, value := range fields {
			if value.path == fieldPath {
				fieldID = makeID(0xc0000001, index*32+fieldIndex)
				break
			}
		}
		if fieldID == "" {
			return nil, fmt.Errorf("missing performance origin field %s", fieldPath)
		}
		return []any{makeID(0xe0000001, index*32+originIndex(fieldPath)), tenantID,
			nullString(paymentID), nullString(invoiceID), nullString(itemID), fieldPath, fieldID,
			makeID(0x50000001, index), createdAt}, nil
	})
}
