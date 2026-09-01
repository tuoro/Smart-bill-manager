package postgresqladapter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const emailSourceProjection = `
	s.id, s.tenant_id, s.display_name, s.mailbox_address_normalized,
	s.imap_host_normalized, s.imap_port, s.transport_security, s.status,
	s.created_by_user_id, s.created_at, s.last_archived_at, s.version,
	(SELECT count(*) FROM email_messages m WHERE m.tenant_id = s.tenant_id AND m.email_source_id = s.id),
	(SELECT count(*) FROM email_attachments a JOIN email_messages m
	 ON m.tenant_id = a.tenant_id AND m.id = a.email_message_id
	 WHERE m.tenant_id = s.tenant_id AND m.email_source_id = s.id),
	(SELECT count(*) FROM email_messages m WHERE m.tenant_id = s.tenant_id AND m.email_source_id = s.id AND m.status = 'blocked')`

func (s *Store) GetEmailSourceRegistrationReplay(
	ctx context.Context,
	tenantID, idempotencyKey string,
) (ports.EmailSourceRegistrationReplay, error) {
	replay, found, err := loadEmailSourceRegistrationReplay(ctx, s.db, tenantID, idempotencyKey)
	if err != nil {
		return ports.EmailSourceRegistrationReplay{}, err
	}
	if !found {
		return ports.EmailSourceRegistrationReplay{}, domain.ErrNotFound
	}
	return replay, nil
}

func (s *Store) ListEmailSources(ctx context.Context, tenantID string) ([]ports.EmailSource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+emailSourceProjection+`
		FROM email_sources s
		WHERE s.tenant_id = ?
		ORDER BY s.created_at, s.id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list email sources: %w", err)
	}
	defer rows.Close()
	items := make([]ports.EmailSource, 0)
	for rows.Next() {
		item, err := scanEmailSource(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate email sources: %w", err)
	}
	return items, nil
}

func (s *Store) GetEmailSource(ctx context.Context, tenantID, sourceID string) (ports.EmailSource, error) {
	item, err := scanEmailSource(s.db.QueryRowContext(ctx, `
		SELECT `+emailSourceProjection+`
		FROM email_sources s
		WHERE s.tenant_id = ? AND s.id = ?
	`, tenantID, sourceID))
	if errors.Is(err, sql.ErrNoRows) {
		return ports.EmailSource{}, domain.ErrNotFound
	}
	return item, err
}

func (t transaction) CreateEmailSource(
	ctx context.Context,
	command ports.CreateEmailSourceCommand,
) (ports.EmailSourceCreateResult, error) {
	canonical, requestHash, err := domain.CanonicalEmailSourceRegistration(domain.EmailSourceRegistration{
		DisplayName: command.Source.DisplayName, MailboxAddress: command.Source.MailboxAddress,
		IMAPHost: command.Source.IMAPHost, IMAPPort: command.Source.IMAPPort,
		TransportSecurity: command.Source.TransportSecurity,
	})
	if err != nil {
		return ports.EmailSourceCreateResult{}, err
	}
	if err := domain.ValidateIdempotencyKey(command.IdempotencyKey); err != nil {
		return ports.EmailSourceCreateResult{}, err
	}
	if command.Source.ID == "" || command.Source.TenantID == "" || command.Source.CreatedByUserID == "" ||
		command.Source.CreatedAt.IsZero() || command.Source.Status != domain.EmailSourcePendingConnection ||
		command.Source.Version != 1 || command.AuditEventID == "" || command.RequestID == "" ||
		canonical.DisplayName != command.Source.DisplayName || canonical.MailboxAddress != command.Source.MailboxAddress ||
		canonical.IMAPHost != command.Source.IMAPHost || requestHash != command.RequestHash {
		return ports.EmailSourceCreateResult{}, domain.ErrInvalidInput
	}
	replay, found, err := loadEmailSourceRegistrationReplay(ctx, t.tx, command.Source.TenantID, command.IdempotencyKey)
	if err != nil {
		return ports.EmailSourceCreateResult{}, err
	}
	if found {
		if replay.RequestHash != command.RequestHash {
			return ports.EmailSourceCreateResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的邮箱来源", domain.ErrConflict)
		}
		return ports.EmailSourceCreateResult{Source: replay.Source, Replayed: true}, nil
	}
	var existingID string
	err = t.tx.QueryRowContext(ctx, `
		SELECT id FROM email_sources
		WHERE tenant_id = ? AND mailbox_address_normalized = ? AND imap_host_normalized = ?
		  AND imap_port = ? AND transport_security = ?
	`, command.Source.TenantID, command.Source.MailboxAddress, command.Source.IMAPHost,
		command.Source.IMAPPort, command.Source.TransportSecurity).Scan(&existingID)
	if err == nil {
		return ports.EmailSourceCreateResult{}, domain.NewRuleError("email_source_exists", "相同邮箱连接身份已存在", domain.ErrConflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ports.EmailSourceCreateResult{}, fmt.Errorf("inspect email source identity: %w", err)
	}
	createdAt := command.Source.CreatedAt.UTC().Format(time.RFC3339Nano)
	metadata, _ := json.Marshal(map[string]string{"protocol": "imap", "status": domain.EmailSourcePendingConnection})
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, ?, 'email_source_registered', 'email_source', ?, ?, ?::jsonb, ?)
	`, command.AuditEventID, command.Source.TenantID, command.Source.CreatedByUserID,
		command.Source.ID, command.RequestID, string(metadata), createdAt); err != nil {
		return ports.EmailSourceCreateResult{}, fmt.Errorf("insert email source audit event: %w", err)
	}
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO email_sources (
			id, tenant_id, display_name, mailbox_address_normalized, imap_host_normalized,
			imap_port, transport_security, status, idempotency_key, request_hash,
			created_by_user_id, created_at, version
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending_connection', ?, ?, ?, ?, 1)
	`, command.Source.ID, command.Source.TenantID, command.Source.DisplayName,
		command.Source.MailboxAddress, command.Source.IMAPHost, command.Source.IMAPPort,
		command.Source.TransportSecurity, command.IdempotencyKey, command.RequestHash,
		command.Source.CreatedByUserID, createdAt); err != nil {
		return ports.EmailSourceCreateResult{}, emailWriteError("insert email source", err)
	}
	return ports.EmailSourceCreateResult{Source: command.Source, Replayed: false}, nil
}

func loadEmailSourceRegistrationReplay(
	ctx context.Context,
	queryer allocationQueryer,
	tenantID, idempotencyKey string,
) (ports.EmailSourceRegistrationReplay, bool, error) {
	row := queryer.QueryRowContext(ctx, `
		SELECT s.request_hash, `+emailSourceProjection+`
		FROM email_sources s
		WHERE s.tenant_id = ? AND s.idempotency_key = ?
	`, tenantID, idempotencyKey)
	var requestHash string
	source, err := scanEmailSourceWithPrefix(row, &requestHash)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.EmailSourceRegistrationReplay{}, false, nil
	}
	if err != nil {
		return ports.EmailSourceRegistrationReplay{}, false, err
	}
	return ports.EmailSourceRegistrationReplay{RequestHash: requestHash, Source: source}, true, nil
}

func scanEmailSource(source scanner) (ports.EmailSource, error) {
	return scanEmailSourceWithPrefix(source)
}

func scanEmailSourceWithPrefix(source scanner, prefix ...any) (ports.EmailSource, error) {
	var item ports.EmailSource
	var createdAt string
	var lastArchived sql.NullString
	destinations := append(prefix,
		&item.ID, &item.TenantID, &item.DisplayName, &item.MailboxAddress,
		&item.IMAPHost, &item.IMAPPort, &item.TransportSecurity, &item.Status,
		&item.CreatedByUserID, &createdAt, &lastArchived, &item.Version,
		&item.MessageCount, &item.AttachmentCount, &item.BlockedCount,
	)
	if err := source.Scan(destinations...); err != nil {
		return ports.EmailSource{}, err
	}
	var err error
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ports.EmailSource{}, fmt.Errorf("parse email source created_at: %w", err)
	}
	if lastArchived.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, lastArchived.String)
		if err != nil {
			return ports.EmailSource{}, fmt.Errorf("parse email source last_archived_at: %w", err)
		}
		item.LastArchivedAt = &parsed
	}
	return item, nil
}

func (s *Store) GetEmailMessageReplay(
	ctx context.Context,
	tenantID, sourceID, externalMessageKey string,
) (ports.EmailMessageReplay, error) {
	replay, found, err := loadEmailMessageReplay(ctx, s.db, tenantID, sourceID, externalMessageKey)
	if err != nil {
		return ports.EmailMessageReplay{}, err
	}
	if !found {
		return ports.EmailMessageReplay{}, domain.ErrNotFound
	}
	return replay, nil
}

func (s *Store) ListEmailMessages(
	ctx context.Context,
	tenantID, sourceID string,
	query ports.EmailMessagePageQuery,
) (ports.EmailMessagePage, error) {
	if query.Limit < 1 || query.Limit > 101 {
		return ports.EmailMessagePage{}, domain.ErrInvalidInput
	}
	arguments := []any{tenantID, sourceID}
	where := ""
	if query.BeforeReceivedAt != nil {
		where = " AND (m.received_at < ? OR (m.received_at = ? AND m.id < ?))"
		value := query.BeforeReceivedAt.UTC().Format(time.RFC3339Nano)
		arguments = append(arguments, value, value, query.BeforeID)
	}
	arguments = append(arguments, query.Limit)
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.email_source_id, m.subject, m.sender_address, m.sent_at,
		       m.received_at, m.status, coalesce(m.safe_error_code, ''),
		       coalesce(m.safe_error_text, ''), m.created_at
		FROM email_messages m
		WHERE m.tenant_id = ? AND m.email_source_id = ?`+where+`
		ORDER BY m.received_at DESC, m.id DESC
		LIMIT ?
	`, arguments...)
	if err != nil {
		return ports.EmailMessagePage{}, fmt.Errorf("list email messages: %w", err)
	}
	defer rows.Close()
	items := make([]ports.EmailMessage, 0)
	for rows.Next() {
		item, err := scanEmailMessage(rows)
		if err != nil {
			return ports.EmailMessagePage{}, err
		}
		item.Attachments, err = loadEmailAttachments(ctx, s.db, tenantID, item.ID)
		if err != nil {
			return ports.EmailMessagePage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ports.EmailMessagePage{}, fmt.Errorf("iterate email messages: %w", err)
	}
	return ports.EmailMessagePage{Items: items}, nil
}

func (s *Store) GetEmailMessageObject(ctx context.Context, tenantID, messageID string) (ports.EmailObject, error) {
	var object ports.EmailObject
	err := s.db.QueryRowContext(ctx, `
		SELECT raw_storage_key, 'message-' || id || '.eml'
		FROM email_messages WHERE tenant_id = ? AND id = ?
	`, tenantID, messageID).Scan(&object.StorageKey, &object.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.EmailObject{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.EmailObject{}, fmt.Errorf("get email message object: %w", err)
	}
	object.MIME = "message/rfc822"
	return object, nil
}

func (s *Store) GetEmailAttachmentObject(ctx context.Context, tenantID, attachmentID string) (ports.EmailObject, error) {
	var object ports.EmailObject
	err := s.db.QueryRowContext(ctx, `
		SELECT storage_key, original_name, declared_mime
		FROM email_attachments
		WHERE tenant_id = ? AND id = ? AND storage_key IS NOT NULL
	`, tenantID, attachmentID).Scan(&object.StorageKey, &object.Name, &object.MIME)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.EmailObject{}, domain.ErrNotFound
	}
	if err != nil {
		return ports.EmailObject{}, fmt.Errorf("get email attachment object: %w", err)
	}
	return object, nil
}

func (t transaction) ArchiveEmailMessage(
	ctx context.Context,
	command ports.EmailArchiveCommand,
) (ports.EmailArchiveResult, error) {
	if err := validateEmailArchiveCommand(command); err != nil {
		return ports.EmailArchiveResult{}, err
	}
	replay, found, err := loadEmailMessageReplay(ctx, t.tx, command.TenantID, command.EmailSourceID, command.ExternalMessageKey)
	if err != nil {
		return ports.EmailArchiveResult{}, err
	}
	if found {
		if replay.RawSHA256 != command.RawSHA256 {
			return ports.EmailArchiveResult{}, domain.NewRuleError("email_message_identity_conflict", "外部邮件身份对应的原文已变化", domain.ErrConflict)
		}
		return replay.Result, nil
	}
	var sourceCreator string
	if err := t.tx.QueryRowContext(ctx, `
		SELECT created_by_user_id FROM email_sources WHERE tenant_id = ? AND id = ?
	`, command.TenantID, command.EmailSourceID).Scan(&sourceCreator); errors.Is(err, sql.ErrNoRows) {
		return ports.EmailArchiveResult{}, domain.ErrNotFound
	} else if err != nil {
		return ports.EmailArchiveResult{}, fmt.Errorf("load email source for archive: %w", err)
	}

	type resolvedAttachment struct {
		draft       ports.EmailAttachmentDraft
		status      string
		documentID  string
		jobID       string
		newDocument bool
	}
	resolved := make([]resolvedAttachment, 0, len(command.Attachments))
	resolvedBySHA := make(map[string]resolvedAttachment)
	newDocuments := make(map[string]ports.EmailAttachmentDraft)
	queued, existing, archivedOnly := 0, 0, 0
	for _, draft := range command.Attachments {
		item := resolvedAttachment{draft: draft, status: draft.ProcessingStatus}
		if draft.Document == nil {
			archivedOnly++
			resolved = append(resolved, item)
			continue
		}
		if previous, ok := resolvedBySHA[draft.SHA256]; ok {
			item.status = domain.EmailAttachmentExisting
			item.documentID = previous.documentID
			item.jobID = previous.jobID
			existing++
			resolved = append(resolved, item)
			continue
		}
		var documentID, jobID string
		err := t.tx.QueryRowContext(ctx, `
			SELECT d.id, j.id
			FROM documents d
			JOIN processing_jobs j ON j.tenant_id = d.tenant_id AND j.document_id = d.id
			WHERE d.tenant_id = ? AND d.sha256 = ?
		`, command.TenantID, draft.SHA256).Scan(&documentID, &jobID)
		switch {
		case err == nil:
			item.status = domain.EmailAttachmentExisting
			item.documentID = documentID
			item.jobID = jobID
			existing++
		case errors.Is(err, sql.ErrNoRows):
			item.status = domain.EmailAttachmentQueued
			item.documentID = draft.Document.ID
			item.jobID = draft.Job.ID
			item.newDocument = true
			newDocuments[draft.Document.ID] = draft
			queued++
		default:
			return ports.EmailArchiveResult{}, fmt.Errorf("resolve email attachment document: %w", err)
		}
		resolvedBySHA[draft.SHA256] = item
		resolved = append(resolved, item)
	}

	createdAt := command.CreatedAt.UTC().Format(time.RFC3339Nano)
	metadata, _ := json.Marshal(map[string]any{
		"status": command.Status, "attachment_count": len(command.Attachments),
		"queued_count": queued, "existing_document_count": existing,
		"archived_only_count": archivedOnly,
	})
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO audit_events (
			id, tenant_id, actor_user_id, action, resource_type, resource_id,
			request_id, safe_metadata_json, created_at
		) VALUES (?, ?, NULL, 'email_message_archived', 'email_message', ?, ?, ?::jsonb, ?)
	`, command.AuditEventID, command.TenantID, command.MessageID,
		command.RequestID, string(metadata), createdAt); err != nil {
		return ports.EmailArchiveResult{}, fmt.Errorf("insert email archive audit event: %w", err)
	}
	var sentAt any
	if command.SentAt != nil {
		sentAt = command.SentAt.UTC().Format(time.RFC3339Nano)
	}
	var safeCode, safeText any
	if command.Status == domain.EmailMessageBlocked {
		safeCode = command.SafeErrorCode
		safeText = command.SafeErrorText
	}
	if _, err := t.tx.ExecContext(ctx, `
		INSERT INTO email_messages (
			id, tenant_id, email_source_id, external_message_key, raw_storage_key,
			raw_sha256, raw_size_bytes, subject, sender_address, sent_at, received_at,
			status, safe_error_code, safe_error_text, audit_event_id, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, command.MessageID, command.TenantID, command.EmailSourceID, command.ExternalMessageKey,
		command.RawStorageKey, command.RawSHA256, command.RawSizeBytes, command.Subject,
		command.SenderAddress, sentAt, command.ReceivedAt.UTC().Format(time.RFC3339Nano),
		command.Status, safeCode, safeText, command.AuditEventID, createdAt); err != nil {
		return ports.EmailArchiveResult{}, emailWriteError("insert email message", err)
	}
	for _, draft := range newDocuments {
		document := *draft.Document
		document.CreatedByUserID = sourceCreator
		if err := t.InsertDocument(ctx, document); err != nil {
			return ports.EmailArchiveResult{}, err
		}
		if err := t.InsertProcessingJob(ctx, *draft.Job); err != nil {
			return ports.EmailArchiveResult{}, err
		}
	}
	result := ports.EmailArchiveResult{
		MessageID: command.MessageID, Status: command.Status, SafeErrorCode: command.SafeErrorCode,
		Attachments: []ports.EmailAttachment{}, CreatedDocumentIDs: []string{},
		CreatedJobIDs: []string{}, Replayed: false,
	}
	for _, item := range resolved {
		var storageKey any
		if item.draft.StorageKey != "" {
			storageKey = item.draft.StorageKey
		}
		var documentID any
		if item.documentID != "" {
			documentID = item.documentID
		}
		var safeReason any
		if item.status == domain.EmailAttachmentArchivedOnly {
			safeReason = item.draft.SafeReasonCode
		}
		if _, err := t.tx.ExecContext(ctx, `
			INSERT INTO email_attachments (
				id, tenant_id, email_message_id, part_index, storage_key, original_name,
				declared_mime, disposition, size_bytes, sha256, processing_status,
				safe_reason_code, document_id, created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.draft.ID, command.TenantID, command.MessageID, item.draft.PartIndex,
			storageKey, item.draft.OriginalName, item.draft.DeclaredMIME, item.draft.Disposition,
			item.draft.SizeBytes, item.draft.SHA256, item.status, safeReason, documentID, createdAt); err != nil {
			return ports.EmailArchiveResult{}, emailWriteError("insert email attachment", err)
		}
		result.Attachments = append(result.Attachments, ports.EmailAttachment{
			ID: item.draft.ID, PartIndex: item.draft.PartIndex, OriginalName: item.draft.OriginalName,
			DeclaredMIME: item.draft.DeclaredMIME, Disposition: item.draft.Disposition,
			SizeBytes: item.draft.SizeBytes, ProcessingStatus: item.status,
			SafeReasonCode: item.draft.SafeReasonCode, DocumentID: item.documentID, JobID: item.jobID,
		})
		if item.newDocument {
			result.CreatedDocumentIDs = append(result.CreatedDocumentIDs, item.documentID)
			result.CreatedJobIDs = append(result.CreatedJobIDs, item.jobID)
		}
	}
	sort.Strings(result.CreatedDocumentIDs)
	sort.Strings(result.CreatedJobIDs)
	if _, err := t.tx.ExecContext(ctx, `
		UPDATE email_sources
		SET status = 'active',
		    last_archived_at = CASE
		        WHEN last_archived_at IS NULL OR last_archived_at < ? THEN ?
		        ELSE last_archived_at
		    END,
		    version = version + 1
		WHERE tenant_id = ? AND id = ?
	`, createdAt, createdAt, command.TenantID, command.EmailSourceID); err != nil {
		return ports.EmailArchiveResult{}, fmt.Errorf("activate archived email source: %w", err)
	}
	return result, nil
}

func (t transaction) CompensateEmailArchive(
	ctx context.Context,
	command ports.CompensateEmailArchiveCommand,
) error {
	var auditID string
	if err := t.tx.QueryRowContext(ctx, `
		SELECT audit_event_id FROM email_messages
		WHERE tenant_id = ? AND email_source_id = ? AND id = ?
	`, command.TenantID, command.EmailSourceID, command.MessageID).Scan(&auditID); errors.Is(err, sql.ErrNoRows) {
		return domain.ErrNotFound
	} else if err != nil {
		return fmt.Errorf("load email archive compensation target: %w", err)
	}
	if auditID != command.AuditEventID {
		return domain.ErrConflict
	}
	if _, err := t.tx.ExecContext(ctx, `
		DELETE FROM email_messages WHERE tenant_id = ? AND email_source_id = ? AND id = ?
	`, command.TenantID, command.EmailSourceID, command.MessageID); err != nil {
		return fmt.Errorf("delete compensated email message: %w", err)
	}
	for _, documentID := range command.CreatedDocumentIDs {
		result, err := t.tx.ExecContext(ctx, `
			DELETE FROM documents
			WHERE tenant_id = ? AND id = ? AND ingestion_kind = 'email_attachment'
			  AND NOT EXISTS (
			      SELECT 1 FROM email_attachments a
			      WHERE a.tenant_id = documents.tenant_id AND a.document_id = documents.id
			  )
		`, command.TenantID, documentID)
		if err != nil {
			return fmt.Errorf("delete compensated email document: %w", err)
		}
		if err := requireAffected(result); err != nil {
			return domain.ErrConflict
		}
	}
	if _, err := t.tx.ExecContext(ctx, `
		DELETE FROM audit_events WHERE tenant_id = ? AND id = ?
	`, command.TenantID, command.AuditEventID); err != nil {
		return fmt.Errorf("delete compensated email audit event: %w", err)
	}
	var latest sql.NullString
	if err := t.tx.QueryRowContext(ctx, `
		SELECT max(created_at) FROM email_messages WHERE tenant_id = ? AND email_source_id = ?
	`, command.TenantID, command.EmailSourceID).Scan(&latest); err != nil {
		return fmt.Errorf("recompute email source archive state: %w", err)
	}
	if latest.Valid {
		if _, err := t.tx.ExecContext(ctx, `
			UPDATE email_sources SET status = 'active', last_archived_at = ?, version = version + 1
			WHERE tenant_id = ? AND id = ?
		`, latest.String, command.TenantID, command.EmailSourceID); err != nil {
			return fmt.Errorf("restore email source archive state: %w", err)
		}
	} else if _, err := t.tx.ExecContext(ctx, `
		UPDATE email_sources SET status = 'pending_connection', last_archived_at = NULL, version = version + 1
		WHERE tenant_id = ? AND id = ?
	`, command.TenantID, command.EmailSourceID); err != nil {
		return fmt.Errorf("restore pending email source state: %w", err)
	}
	return nil
}

func validateEmailArchiveCommand(command ports.EmailArchiveCommand) error {
	if command.TenantID == "" || command.EmailSourceID == "" || command.MessageID == "" ||
		command.RawStorageKey != "tenants/"+command.TenantID+"/email-messages/"+command.MessageID+"/raw.eml" ||
		command.AuditEventID == "" || command.RequestID == "" ||
		command.CreatedAt.IsZero() || command.ReceivedAt.IsZero() ||
		command.RawSizeBytes < 1 || command.RawSizeBytes > domain.MaxEmailMessageBytes ||
		!domain.ValidSHA256Hex(command.RawSHA256) || len(command.Attachments) > domain.MaxEmailAttachments ||
		!validEmailProjection(command.Subject, 500, true) ||
		!validEmailProjection(command.SenderAddress, 254, true) {
		return domain.ErrInvalidInput
	}
	if err := domain.ValidateExternalMessageKey(command.ExternalMessageKey); err != nil {
		return err
	}
	if command.Status == domain.EmailMessageBlocked {
		if !validEmailProjection(command.SafeErrorCode, 100, false) ||
			!validEmailProjection(command.SafeErrorText, 200, false) || len(command.Attachments) != 0 {
			return domain.ErrInvalidInput
		}
	} else if command.Status != domain.EmailMessageArchived || command.SafeErrorCode != "" || command.SafeErrorText != "" {
		return domain.ErrInvalidInput
	}
	seenParts := make(map[int]struct{}, len(command.Attachments))
	seenIDs := make(map[string]struct{}, len(command.Attachments))
	for _, item := range command.Attachments {
		if item.ID == "" || item.PartIndex < 1 || item.PartIndex > domain.MaxEmailAttachments ||
			!validEmailProjection(item.OriginalName, 200, false) ||
			!validEmailProjection(item.DeclaredMIME, 200, false) ||
			(item.Disposition != "attachment" && item.Disposition != "inline") ||
			item.SizeBytes < 0 || item.SizeBytes > domain.MaxEmailMessageBytes || !domain.ValidSHA256Hex(item.SHA256) {
			return domain.ErrInvalidInput
		}
		if _, duplicate := seenParts[item.PartIndex]; duplicate {
			return domain.ErrInvalidInput
		}
		if _, duplicate := seenIDs[item.ID]; duplicate {
			return domain.ErrInvalidInput
		}
		seenParts[item.PartIndex] = struct{}{}
		seenIDs[item.ID] = struct{}{}
		expectedStorageKey := "tenants/" + command.TenantID + "/email-messages/" + command.MessageID +
			"/attachments/" + item.ID + "/content"
		if item.SizeBytes == 0 && item.StorageKey != "" ||
			item.SizeBytes > 0 && item.StorageKey != expectedStorageKey {
			return domain.ErrInvalidInput
		}
		if item.Document == nil {
			if item.ProcessingStatus != domain.EmailAttachmentArchivedOnly ||
				!validEmailProjection(item.SafeReasonCode, 100, false) || item.Job != nil {
				return domain.ErrInvalidInput
			}
			continue
		}
		if item.Job == nil || item.ProcessingStatus != "" || item.SafeReasonCode != "" ||
			item.Document.ID == "" || item.Job.ID == "" ||
			item.Document.TenantID != command.TenantID || item.Document.StorageKey != item.StorageKey ||
			item.Document.SHA256 != item.SHA256 || item.Document.IngestionKind != domain.DocumentIngestionEmail ||
			item.Document.OriginalObjectOwner != domain.DocumentObjectOwnerEmail ||
			item.Job.TenantID != command.TenantID || item.Job.DocumentID != item.Document.ID {
			return domain.ErrInvalidInput
		}
	}
	return nil
}

func validEmailProjection(value string, maximum int, allowEmpty bool) bool {
	if !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	if length > maximum || (!allowEmpty && length == 0) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func loadEmailMessageReplay(
	ctx context.Context,
	queryer allocationQueryer,
	tenantID, sourceID, externalMessageKey string,
) (ports.EmailMessageReplay, bool, error) {
	var replay ports.EmailMessageReplay
	var messageID, status, safeCode string
	err := queryer.QueryRowContext(ctx, `
		SELECT id, raw_sha256, status, coalesce(safe_error_code, '')
		FROM email_messages
		WHERE tenant_id = ? AND email_source_id = ? AND external_message_key = ?
	`, tenantID, sourceID, externalMessageKey).Scan(&messageID, &replay.RawSHA256, &status, &safeCode)
	if errors.Is(err, sql.ErrNoRows) {
		return ports.EmailMessageReplay{}, false, nil
	}
	if err != nil {
		return ports.EmailMessageReplay{}, false, fmt.Errorf("load email message replay: %w", err)
	}
	attachments, err := loadEmailAttachments(ctx, queryer, tenantID, messageID)
	if err != nil {
		return ports.EmailMessageReplay{}, false, err
	}
	replay.Result = ports.EmailArchiveResult{
		MessageID: messageID, Status: status, SafeErrorCode: safeCode,
		Attachments: attachments, CreatedDocumentIDs: []string{}, CreatedJobIDs: []string{}, Replayed: true,
	}
	for _, attachment := range attachments {
		if attachment.ProcessingStatus == domain.EmailAttachmentQueued && attachment.DocumentID != "" {
			replay.Result.CreatedDocumentIDs = append(replay.Result.CreatedDocumentIDs, attachment.DocumentID)
			if attachment.JobID != "" {
				replay.Result.CreatedJobIDs = append(replay.Result.CreatedJobIDs, attachment.JobID)
			}
		}
	}
	sort.Strings(replay.Result.CreatedDocumentIDs)
	sort.Strings(replay.Result.CreatedJobIDs)
	return replay, true, nil
}

func loadEmailAttachments(
	ctx context.Context,
	queryer allocationQueryer,
	tenantID, messageID string,
) ([]ports.EmailAttachment, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT a.id, a.part_index, a.original_name, a.declared_mime, a.disposition,
		       a.size_bytes,
		       CASE WHEN a.document_id IS NULL AND a.processing_status <> 'archived_only'
		            THEN 'archived_only' ELSE a.processing_status END,
		       CASE WHEN a.document_id IS NULL AND a.processing_status <> 'archived_only'
		            THEN 'document_deleted' ELSE coalesce(a.safe_reason_code, '') END,
		       coalesce(a.document_id, ''),
		       coalesce((SELECT j.id FROM processing_jobs j
		                 WHERE j.tenant_id = a.tenant_id AND j.document_id = a.document_id), '')
		FROM email_attachments a
		WHERE a.tenant_id = ? AND a.email_message_id = ?
		ORDER BY a.part_index
	`, tenantID, messageID)
	if err != nil {
		return nil, fmt.Errorf("list email attachments: %w", err)
	}
	defer rows.Close()
	items := make([]ports.EmailAttachment, 0)
	for rows.Next() {
		var item ports.EmailAttachment
		if err := rows.Scan(
			&item.ID, &item.PartIndex, &item.OriginalName, &item.DeclaredMIME,
			&item.Disposition, &item.SizeBytes, &item.ProcessingStatus,
			&item.SafeReasonCode, &item.DocumentID, &item.JobID,
		); err != nil {
			return nil, fmt.Errorf("scan email attachment: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanEmailMessage(source scanner) (ports.EmailMessage, error) {
	var item ports.EmailMessage
	var sentAt sql.NullString
	var receivedAt, createdAt string
	if err := source.Scan(
		&item.ID, &item.EmailSourceID, &item.Subject, &item.SenderAddress, &sentAt,
		&receivedAt, &item.Status, &item.SafeErrorCode, &item.SafeErrorText, &createdAt,
	); err != nil {
		return ports.EmailMessage{}, err
	}
	var err error
	item.ReceivedAt, err = time.Parse(time.RFC3339Nano, receivedAt)
	if err != nil {
		return ports.EmailMessage{}, fmt.Errorf("parse email received_at: %w", err)
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return ports.EmailMessage{}, fmt.Errorf("parse email created_at: %w", err)
	}
	if sentAt.Valid {
		parsed, err := time.Parse(time.RFC3339Nano, sentAt.String)
		if err != nil {
			return ports.EmailMessage{}, fmt.Errorf("parse email sent_at: %w", err)
		}
		item.SentAt = &parsed
	}
	return item, nil
}

func emailWriteError(operation string, err error) error {
	message := err.Error()
	switch {
	case strings.Contains(message, "email_sources.tenant_id, email_sources.idempotency_key"):
		return domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的邮箱来源", domain.ErrConflict)
	case strings.Contains(message, "email_sources.tenant_id, email_sources.mailbox_address_normalized"):
		return domain.NewRuleError("email_source_exists", "相同邮箱连接身份已存在", domain.ErrConflict)
	case strings.Contains(message, "email_messages.tenant_id, email_messages.email_source_id"):
		return domain.NewRuleError("email_message_identity_conflict", "外部邮件身份已存在", domain.ErrConflict)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}
