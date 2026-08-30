package emails

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const emailMessageCursorVersion = "email-message-cursor/1"

type Service struct {
	repository ports.EmailRepository
	tx         ports.TransactionManager
	objects    ports.ObjectStore
	inspector  ports.DocumentInspector
	parser     ports.EmailParser
	ids        ports.IDGenerator
	clock      ports.Clock
}

type RegisterInput struct {
	Tenant         domain.TenantContext
	Registration   domain.EmailSourceRegistration
	IdempotencyKey string
	RequestID      string
}

type ArchiveInput struct {
	TenantID           string
	EmailSourceID      string
	ExternalMessageKey string
	ReceivedAt         time.Time
	Raw                io.Reader
	RequestID          string
}

type ArchivedContent struct {
	Name string
	MIME string
	Body io.ReadCloser
}

func NewService(
	repository ports.EmailRepository,
	tx ports.TransactionManager,
	objects ports.ObjectStore,
	inspector ports.DocumentInspector,
	parser ports.EmailParser,
	ids ports.IDGenerator,
	clock ports.Clock,
) Service {
	return Service{
		repository: repository, tx: tx, objects: objects, inspector: inspector,
		parser: parser, ids: ids, clock: clock,
	}
}

func (s Service) Register(ctx context.Context, input RegisterInput) (ports.EmailSourceCreateResult, error) {
	if err := input.Tenant.Require(domain.CapabilityEmailSourcesManage); err != nil {
		return ports.EmailSourceCreateResult{}, err
	}
	if err := domain.ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return ports.EmailSourceCreateResult{}, err
	}
	if input.RequestID == "" {
		return ports.EmailSourceCreateResult{}, errors.New("request id is required")
	}
	canonical, requestHash, err := domain.CanonicalEmailSourceRegistration(input.Registration)
	if err != nil {
		return ports.EmailSourceCreateResult{}, err
	}
	replay, replayErr := s.repository.GetEmailSourceRegistrationReplay(ctx, input.Tenant.TenantID, input.IdempotencyKey)
	if replayErr == nil {
		if replay.RequestHash != requestHash {
			return ports.EmailSourceCreateResult{}, domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的邮箱来源", domain.ErrConflict)
		}
		return ports.EmailSourceCreateResult{Source: replay.Source, Replayed: true}, nil
	}
	if !errors.Is(replayErr, domain.ErrNotFound) {
		return ports.EmailSourceCreateResult{}, replayErr
	}
	sourceID, err := s.ids.NewID()
	if err != nil {
		return ports.EmailSourceCreateResult{}, err
	}
	auditID, err := s.ids.NewID()
	if err != nil {
		return ports.EmailSourceCreateResult{}, err
	}
	command := ports.CreateEmailSourceCommand{
		Source: ports.EmailSource{
			ID: sourceID, TenantID: input.Tenant.TenantID, DisplayName: canonical.DisplayName,
			MailboxAddress: canonical.MailboxAddress, IMAPHost: canonical.IMAPHost,
			IMAPPort: canonical.IMAPPort, TransportSecurity: canonical.TransportSecurity,
			Status: domain.EmailSourcePendingConnection, CreatedByUserID: input.Tenant.UserID,
			CreatedAt: s.clock.Now(), Version: 1,
		},
		IdempotencyKey: input.IdempotencyKey, RequestHash: requestHash,
		AuditEventID: auditID, RequestID: input.RequestID,
	}
	var result ports.EmailSourceCreateResult
	err = s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		var operationErr error
		result, operationErr = transaction.CreateEmailSource(ctx, command)
		return operationErr
	})
	if err == nil {
		return result, nil
	}
	recovered, recoveredErr := s.repository.GetEmailSourceRegistrationReplay(ctx, input.Tenant.TenantID, input.IdempotencyKey)
	if recoveredErr == nil && recovered.RequestHash == requestHash {
		return ports.EmailSourceCreateResult{Source: recovered.Source, Replayed: true}, nil
	}
	return ports.EmailSourceCreateResult{}, err
}

func (s Service) ListSources(ctx context.Context, tenant domain.TenantContext) ([]ports.EmailSource, error) {
	if err := tenant.Require(domain.CapabilityEmailArchiveRead); err != nil {
		return nil, err
	}
	items, err := s.repository.ListEmailSources(ctx, tenant.TenantID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []ports.EmailSource{}
	}
	return items, nil
}

func (s Service) ListMessages(
	ctx context.Context,
	tenant domain.TenantContext,
	sourceID, cursor string,
	limit int,
) (ports.EmailMessagePage, error) {
	if err := tenant.Require(domain.CapabilityEmailArchiveRead); err != nil {
		return ports.EmailMessagePage{}, err
	}
	if sourceID == "" {
		return ports.EmailMessagePage{}, domain.ErrInvalidInput
	}
	if _, err := s.repository.GetEmailSource(ctx, tenant.TenantID, sourceID); err != nil {
		return ports.EmailMessagePage{}, err
	}
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 {
		return ports.EmailMessagePage{}, domain.NewRuleError("invalid_email_page_limit", "邮件分页数量必须为 1–100", domain.ErrInvalidInput)
	}
	query := ports.EmailMessagePageQuery{Limit: limit + 1}
	if cursor != "" {
		receivedAt, id, err := decodeEmailCursor(cursor)
		if err != nil {
			return ports.EmailMessagePage{}, err
		}
		query.BeforeReceivedAt = &receivedAt
		query.BeforeID = id
	}
	page, err := s.repository.ListEmailMessages(ctx, tenant.TenantID, sourceID, query)
	if err != nil {
		return ports.EmailMessagePage{}, err
	}
	if page.Items == nil {
		page.Items = []ports.EmailMessage{}
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor, err = encodeEmailCursor(last.ReceivedAt, last.ID)
		if err != nil {
			return ports.EmailMessagePage{}, err
		}
	}
	return page, nil
}

func (s Service) OpenMessage(
	ctx context.Context,
	tenant domain.TenantContext,
	messageID string,
) (ArchivedContent, error) {
	return s.openObject(ctx, tenant, messageID, s.repository.GetEmailMessageObject)
}

func (s Service) OpenAttachment(
	ctx context.Context,
	tenant domain.TenantContext,
	attachmentID string,
) (ArchivedContent, error) {
	return s.openObject(ctx, tenant, attachmentID, s.repository.GetEmailAttachmentObject)
}

func (s Service) openObject(
	ctx context.Context,
	tenant domain.TenantContext,
	resourceID string,
	lookup func(context.Context, string, string) (ports.EmailObject, error),
) (ArchivedContent, error) {
	if err := tenant.Require(domain.CapabilityEmailArchiveRead); err != nil {
		return ArchivedContent{}, err
	}
	if resourceID == "" {
		return ArchivedContent{}, domain.ErrInvalidInput
	}
	object, err := lookup(ctx, tenant.TenantID, resourceID)
	if err != nil {
		return ArchivedContent{}, err
	}
	body, err := s.objects.Open(ctx, object.StorageKey)
	if err != nil {
		return ArchivedContent{}, err
	}
	return ArchivedContent{Name: object.Name, MIME: object.MIME, Body: body}, nil
}

func (s Service) Archive(ctx context.Context, input ArchiveInput) (ports.EmailArchiveResult, error) {
	if input.TenantID == "" || input.EmailSourceID == "" || input.Raw == nil ||
		input.RequestID == "" || input.ReceivedAt.IsZero() {
		return ports.EmailArchiveResult{}, domain.ErrInvalidInput
	}
	if err := domain.ValidateExternalMessageKey(input.ExternalMessageKey); err != nil {
		return ports.EmailArchiveResult{}, err
	}
	source, err := s.repository.GetEmailSource(ctx, input.TenantID, input.EmailSourceID)
	if err != nil {
		return ports.EmailArchiveResult{}, err
	}
	raw, err := readEmailRaw(input.Raw)
	if err != nil {
		return ports.EmailArchiveResult{}, err
	}
	rawStaged, err := s.objects.Stage(ctx, bytes.NewReader(raw), domain.MaxEmailMessageBytes)
	if err != nil {
		return ports.EmailArchiveResult{}, err
	}
	pending := []pendingObject{{staged: rawStaged}}
	defer func() {
		for _, object := range pending {
			_ = s.objects.Abort(context.WithoutCancel(ctx), object.staged)
		}
	}()
	replay, replayErr := s.repository.GetEmailMessageReplay(ctx, input.TenantID, input.EmailSourceID, input.ExternalMessageKey)
	if replayErr == nil {
		if replay.RawSHA256 != rawStaged.SHA256 {
			return ports.EmailArchiveResult{}, domain.NewRuleError("email_message_identity_conflict", "外部邮件身份对应的原文已变化", domain.ErrConflict)
		}
		return replay.Result, nil
	}
	if !errors.Is(replayErr, domain.ErrNotFound) {
		return ports.EmailArchiveResult{}, replayErr
	}

	messageID, err := s.ids.NewID()
	if err != nil {
		return ports.EmailArchiveResult{}, err
	}
	auditID, err := s.ids.NewID()
	if err != nil {
		return ports.EmailArchiveResult{}, err
	}
	createdAt := s.clock.Now()
	parsed := s.parser.Parse(raw)
	command := ports.EmailArchiveCommand{
		TenantID: input.TenantID, EmailSourceID: input.EmailSourceID,
		ExternalMessageKey: input.ExternalMessageKey,
		RawStorageKey:      "tenants/" + input.TenantID + "/email-messages/" + messageID + "/raw.eml",
		RawSHA256:          rawStaged.SHA256, RawSizeBytes: rawStaged.Size, MessageID: messageID,
		Subject: parsed.Subject, SenderAddress: parsed.SenderAddress, SentAt: parsed.SentAt,
		ReceivedAt: input.ReceivedAt.UTC(), Status: domain.EmailMessageArchived,
		Attachments: []ports.EmailAttachmentDraft{}, AuditEventID: auditID,
		RequestID: input.RequestID, CreatedAt: createdAt,
	}
	pending[0].storageKey = command.RawStorageKey
	if parsed.BlockedCode != "" {
		command.Status = domain.EmailMessageBlocked
		command.SafeErrorCode = parsed.BlockedCode
		command.SafeErrorText = parsed.SafeErrorText
	} else {
		for _, attachment := range parsed.Attachments {
			draft, staged, err := s.prepareAttachment(ctx, source, messageID, createdAt, attachment)
			if err != nil {
				return ports.EmailArchiveResult{}, err
			}
			command.Attachments = append(command.Attachments, draft)
			if staged != nil {
				pending = append(pending, *staged)
			}
		}
	}
	var result ports.EmailArchiveResult
	err = s.tx.WithinTransaction(ctx, func(transaction ports.Transaction) error {
		var operationErr error
		result, operationErr = transaction.ArchiveEmailMessage(ctx, command)
		return operationErr
	})
	if err != nil {
		recovered, recoveredErr := s.repository.GetEmailMessageReplay(ctx, input.TenantID, input.EmailSourceID, input.ExternalMessageKey)
		if recoveredErr == nil && recovered.RawSHA256 == rawStaged.SHA256 {
			return recovered.Result, nil
		}
		return ports.EmailArchiveResult{}, err
	}
	if result.Replayed {
		return result, nil
	}
	for index := range pending {
		if err := s.objects.Commit(ctx, pending[index].staged, pending[index].storageKey); err != nil {
			return ports.EmailArchiveResult{}, s.compensateObjectFailure(ctx, command, result, pending, err)
		}
	}
	return result, nil
}

type pendingObject struct {
	staged     ports.StagedObject
	storageKey string
}

func (s Service) prepareAttachment(
	ctx context.Context,
	source ports.EmailSource,
	messageID string,
	createdAt time.Time,
	attachment ports.ParsedEmailAttachment,
) (ports.EmailAttachmentDraft, *pendingObject, error) {
	attachmentID, err := s.ids.NewID()
	if err != nil {
		return ports.EmailAttachmentDraft{}, nil, err
	}
	digest := sha256.Sum256(attachment.Content)
	declaredMIME, mimeReason := safeAttachmentMIME(attachment.MIME)
	draft := ports.EmailAttachmentDraft{
		ID: attachmentID, PartIndex: attachment.PartIndex, DeclaredMIME: declaredMIME,
		Disposition: attachment.Disposition, SizeBytes: int64(len(attachment.Content)),
		SHA256: hex.EncodeToString(digest[:]), ProcessingStatus: domain.EmailAttachmentArchivedOnly,
	}
	name, nameErr := documents.NormalizeDocumentName(attachment.Name)
	if nameErr != nil {
		name = fmt.Sprintf("附件-%03d", attachment.PartIndex)
		draft.SafeReasonCode = "invalid_attachment_name"
	}
	if draft.SafeReasonCode == "" {
		draft.SafeReasonCode = mimeReason
	}
	draft.OriginalName = name
	var object *pendingObject
	if len(attachment.Content) == 0 {
		if draft.SafeReasonCode == "" {
			draft.SafeReasonCode = "empty_attachment"
		}
		return draft, nil, nil
	}
	staged, err := s.objects.Stage(ctx, bytes.NewReader(attachment.Content), domain.MaxEmailMessageBytes)
	if err != nil {
		return ports.EmailAttachmentDraft{}, nil, err
	}
	draft.SHA256 = staged.SHA256
	draft.SizeBytes = staged.Size
	draft.StorageKey = "tenants/" + source.TenantID + "/email-messages/" + messageID + "/attachments/" + attachmentID + "/content"
	object = &pendingObject{staged: staged, storageKey: draft.StorageKey}
	if draft.SafeReasonCode != "" {
		return draft, object, nil
	}
	if !processableAttachmentMIME(draft.DeclaredMIME) {
		draft.SafeReasonCode = "unsupported_attachment_type"
		return draft, object, nil
	}
	if staged.Size > ports.MaxDocumentBytes {
		draft.SafeReasonCode = "attachment_too_large_for_processing"
		return draft, object, nil
	}
	inspection, err := s.inspector.InspectStaged(ctx, staged, name, draft.DeclaredMIME)
	if err != nil {
		draft.SafeReasonCode = safeAttachmentInspectionCode(err)
		return draft, object, nil
	}
	documentID, err := s.ids.NewID()
	if err != nil {
		return ports.EmailAttachmentDraft{}, object, err
	}
	jobID, err := s.ids.NewID()
	if err != nil {
		return ports.EmailAttachmentDraft{}, object, err
	}
	draft.ProcessingStatus = ""
	draft.SafeReasonCode = ""
	draft.Document = &ports.Document{
		ID: documentID, TenantID: source.TenantID, StorageKey: draft.StorageKey,
		OriginalName: name, DeclaredMIME: draft.DeclaredMIME, DetectedMIME: inspection.DetectedMIME,
		SizeBytes: staged.Size, SHA256: staged.SHA256, PageCount: inspection.PageCount,
		Status: "stored", IngestionKind: domain.DocumentIngestionEmail,
		OriginalObjectOwner: domain.DocumentObjectOwnerEmail,
		CreatedByUserID:     source.CreatedByUserID, CreatedAt: createdAt,
	}
	draft.Job = &ports.ProcessingJob{
		ID: jobID, TenantID: source.TenantID, DocumentID: documentID,
		Kind: "document_process", Status: domain.JobQueued, AttemptCount: 0,
		CreatedAt: createdAt, Version: 1,
	}
	return draft, object, nil
}

func (s Service) compensateObjectFailure(
	ctx context.Context,
	command ports.EmailArchiveCommand,
	result ports.EmailArchiveResult,
	objects []pendingObject,
	commitErr error,
) error {
	compensationErr := s.tx.WithinTransaction(context.WithoutCancel(ctx), func(transaction ports.Transaction) error {
		return transaction.CompensateEmailArchive(context.WithoutCancel(ctx), ports.CompensateEmailArchiveCommand{
			TenantID: command.TenantID, EmailSourceID: command.EmailSourceID,
			MessageID: command.MessageID, AuditEventID: command.AuditEventID,
			CreatedDocumentIDs: result.CreatedDocumentIDs,
		})
	})
	cleanupErrors := []error{fmt.Errorf("commit email archive object: %w", commitErr)}
	if compensationErr != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("compensate email archive metadata: %w", compensationErr))
	}
	for _, object := range objects {
		if object.storageKey != "" {
			if err := s.objects.Delete(context.WithoutCancel(ctx), object.storageKey); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("delete email archive object: %w", err))
			}
		}
	}
	return errors.Join(cleanupErrors...)
}

func readEmailRaw(source io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(source, domain.MaxEmailMessageBytes+1))
	if err != nil {
		return nil, domain.NewRuleError("email_message_read_failed", "邮件原文读取失败", domain.ErrInvalidInput)
	}
	if int64(len(raw)) > domain.MaxEmailMessageBytes {
		return nil, domain.NewRuleError("email_message_too_large", "邮件原文不能超过 32 MiB", domain.ErrPayloadTooLarge)
	}
	if len(raw) == 0 {
		return nil, domain.NewRuleError("empty_email_message", "邮件原文不能为空", domain.ErrInvalidInput)
	}
	return raw, nil
}

func processableAttachmentMIME(value string) bool {
	switch value {
	case "image/jpeg", "image/png", "image/webp", "application/pdf":
		return true
	default:
		return false
	}
}

func safeAttachmentMIME(value string) (string, string) {
	value = strings.ToLower(strings.TrimSpace(value))
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil || len(parameters) != 0 || len(mediaType) > 200 {
		return "application/octet-stream", "invalid_attachment_mime"
	}
	return mediaType, ""
}

func safeAttachmentInspectionCode(err error) string {
	var rule *domain.RuleError
	if errors.As(err, &rule) {
		switch rule.Code {
		case "document_signature_mismatch", "corrupt_document", "invalid_image_dimensions",
			"pdf_inspection_timeout", "corrupt_pdf", "encrypted_pdf", "pdf_page_limit":
			return rule.Code
		}
	}
	return "attachment_inspection_failed"
}

type emailCursor struct {
	Version    string `json:"version"`
	ReceivedAt string `json:"received_at"`
	ID         string `json:"id"`
}

func encodeEmailCursor(receivedAt time.Time, id string) (string, error) {
	payload, err := json.Marshal(emailCursor{
		Version:    emailMessageCursorVersion,
		ReceivedAt: receivedAt.UTC().Format(time.RFC3339Nano), ID: id,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeEmailCursor(value string) (time.Time, string, error) {
	if len(value) > 1024 {
		return time.Time{}, "", invalidEmailCursor()
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, "", invalidEmailCursor()
	}
	var cursor emailCursor
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil || cursor.Version != emailMessageCursorVersion || strings.TrimSpace(cursor.ID) == "" {
		return time.Time{}, "", invalidEmailCursor()
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return time.Time{}, "", invalidEmailCursor()
	}
	receivedAt, err := time.Parse(time.RFC3339Nano, cursor.ReceivedAt)
	if err != nil {
		return time.Time{}, "", invalidEmailCursor()
	}
	return receivedAt.UTC(), cursor.ID, nil
}

func invalidEmailCursor() error {
	return domain.NewRuleError("invalid_email_cursor", "邮件分页游标不合法", domain.ErrInvalidInput)
}
