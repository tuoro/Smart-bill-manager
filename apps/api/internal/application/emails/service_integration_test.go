package emails

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/emailmime"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/sqlite"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const syntheticOnePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func TestEmailArchiveLifecyclePreservesSourceAndDocumentBoundaries(t *testing.T) {
	fixture := newEmailIntegrationFixture(t)
	source := fixture.registerSource(t, "email-source-lifecycle")
	if source.DisplayName != "合成财务邮箱" || source.MailboxAddress != "finance@example.invalid" ||
		source.IMAPHost != "imap.example.invalid" || source.Status != domain.EmailSourcePendingConnection {
		t.Fatalf("canonical source = %#v", source)
	}
	registrationReplay, err := fixture.service.Register(context.Background(), RegisterInput{
		Tenant: fixture.tenant, Registration: validEmailRegistration(),
		IdempotencyKey: "email-source-lifecycle", RequestID: "email-source-replay",
	})
	if err != nil || !registrationReplay.Replayed || registrationReplay.Source.ID != source.ID {
		t.Fatalf("source replay = %#v, error=%v", registrationReplay, err)
	}
	changed := validEmailRegistration()
	changed.DisplayName = "另一个名称"
	_, err = fixture.service.Register(context.Background(), RegisterInput{
		Tenant: fixture.tenant, Registration: changed,
		IdempotencyKey: "email-source-lifecycle", RequestID: "email-source-conflict",
	})
	var sourceConflict *domain.RuleError
	if !errors.As(err, &sourceConflict) || sourceConflict.Code != "idempotency_key_conflict" {
		t.Fatalf("source idempotency conflict = %v", err)
	}
	_, err = fixture.service.Register(context.Background(), RegisterInput{
		Tenant: fixture.tenant, Registration: validEmailRegistration(),
		IdempotencyKey: "email-source-duplicate", RequestID: "email-source-duplicate-request",
	})
	if !errors.As(err, &sourceConflict) || sourceConflict.Code != "email_source_exists" {
		t.Fatalf("source identity conflict = %v", err)
	}
	png, err := base64.StdEncoding.DecodeString(syntheticOnePixelPNG)
	if err != nil {
		t.Fatal(err)
	}
	raw := syntheticMixedEmail(png)
	receivedAt := fixture.now.Add(time.Hour)
	result, err := fixture.service.Archive(context.Background(), ArchiveInput{
		TenantID: fixture.tenant.TenantID, EmailSourceID: source.ID,
		ExternalMessageKey: strings.Repeat("a", 64), ReceivedAt: receivedAt,
		Raw: bytes.NewReader(raw), RequestID: "archive-lifecycle-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Replayed || result.Status != domain.EmailMessageArchived ||
		len(result.Attachments) != 4 || len(result.CreatedDocumentIDs) != 1 || len(result.CreatedJobIDs) != 1 {
		t.Fatalf("archive result = %#v", result)
	}
	queued := findAttachmentByName(t, result.Attachments, "invoice.png")
	unsupported := findAttachmentByName(t, result.Attachments, "notes.txt")
	empty := findAttachmentByName(t, result.Attachments, "empty.txt")
	forged := findAttachmentByName(t, result.Attachments, "forged.png")
	if queued.ProcessingStatus != domain.EmailAttachmentQueued || queued.DocumentID == "" || queued.JobID == "" {
		t.Fatalf("queued attachment = %#v", queued)
	}
	if unsupported.ProcessingStatus != domain.EmailAttachmentArchivedOnly || unsupported.SafeReasonCode != "unsupported_attachment_type" {
		t.Fatalf("unsupported attachment = %#v", unsupported)
	}
	if empty.ProcessingStatus != domain.EmailAttachmentArchivedOnly || empty.SafeReasonCode != "empty_attachment" {
		t.Fatalf("empty attachment = %#v", empty)
	}
	if forged.ProcessingStatus != domain.EmailAttachmentArchivedOnly || forged.SafeReasonCode != "document_signature_mismatch" {
		t.Fatalf("forged attachment = %#v", forged)
	}

	replayed, err := fixture.service.Archive(context.Background(), ArchiveInput{
		TenantID: fixture.tenant.TenantID, EmailSourceID: source.ID,
		ExternalMessageKey: strings.Repeat("a", 64), ReceivedAt: receivedAt,
		Raw: bytes.NewReader(raw), RequestID: "archive-lifecycle-replay",
	})
	if err != nil || !replayed.Replayed || replayed.MessageID != result.MessageID {
		t.Fatalf("replay = %#v, error=%v", replayed, err)
	}
	_, err = fixture.service.Archive(context.Background(), ArchiveInput{
		TenantID: fixture.tenant.TenantID, EmailSourceID: source.ID,
		ExternalMessageKey: strings.Repeat("a", 64), ReceivedAt: receivedAt,
		Raw: bytes.NewReader(append(append([]byte(nil), raw...), '\n')), RequestID: "archive-lifecycle-conflict",
	})
	var conflict *domain.RuleError
	if !errors.As(err, &conflict) || conflict.Code != "email_message_identity_conflict" || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("identity conflict = %v", err)
	}

	second, err := fixture.service.Archive(context.Background(), ArchiveInput{
		TenantID: fixture.tenant.TenantID, EmailSourceID: source.ID,
		ExternalMessageKey: strings.Repeat("b", 64), ReceivedAt: receivedAt.Add(time.Minute),
		Raw: bytes.NewReader(syntheticImageEmail("第二封合成邮件", png)), RequestID: "archive-lifecycle-2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Attachments) != 1 || second.Attachments[0].ProcessingStatus != domain.EmailAttachmentExisting ||
		second.Attachments[0].DocumentID != queued.DocumentID || len(second.CreatedDocumentIDs) != 0 {
		t.Fatalf("exact duplicate attachment = %#v", second)
	}

	sources, err := fixture.service.ListSources(context.Background(), fixture.tenant)
	if err != nil || len(sources) != 1 {
		t.Fatalf("sources = %#v, error=%v", sources, err)
	}
	if sources[0].Status != domain.EmailSourceActive || sources[0].MessageCount != 2 ||
		sources[0].AttachmentCount != 5 || sources[0].BlockedCount != 0 || sources[0].LastArchivedAt == nil {
		t.Fatalf("source projection = %#v", sources[0])
	}
	jobs, err := fixture.store.ListJobs(context.Background(), fixture.tenant.TenantID, nil)
	if err != nil || len(jobs) != 1 || jobs[0].IngestionKind != domain.DocumentIngestionEmail {
		t.Fatalf("email document jobs = %#v, error=%v", jobs, err)
	}
	var runs, claims, payments, invoices int
	if err := fixture.store.DB().QueryRow(`
		SELECT
			(SELECT count(*) FROM ai_runs WHERE tenant_id = ?),
			(SELECT count(*) FROM claim_sets WHERE tenant_id = ?),
			(SELECT count(*) FROM payments WHERE tenant_id = ?),
			(SELECT count(*) FROM invoices WHERE tenant_id = ?)
	`, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID, fixture.tenant.TenantID).Scan(
		&runs, &claims, &payments, &invoices,
	); err != nil {
		t.Fatal(err)
	}
	if runs != 0 || claims != 0 || payments != 0 || invoices != 0 {
		t.Fatalf("archive bypassed processing/review boundary: runs=%d claims=%d payments=%d invoices=%d",
			runs, claims, payments, invoices)
	}

	messageContent, err := fixture.service.OpenMessage(context.Background(), fixture.tenant, result.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	downloadedRaw, err := ioReadAllAndClose(messageContent.Body)
	if err != nil || !bytes.Equal(downloadedRaw, raw) || messageContent.MIME != "message/rfc822" {
		t.Fatalf("raw archive mismatch: mime=%q error=%v", messageContent.MIME, err)
	}
	attachmentContent, err := fixture.service.OpenAttachment(context.Background(), fixture.tenant, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	downloadedPNG, err := ioReadAllAndClose(attachmentContent.Body)
	if err != nil || !bytes.Equal(downloadedPNG, png) {
		t.Fatalf("attachment archive mismatch: error=%v", err)
	}
	if _, err := fixture.service.OpenAttachment(context.Background(), fixture.tenant, empty.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("empty attachment download error = %v", err)
	}

	foreign := domain.TenantContext{TenantID: "00000000-0000-4000-8000-000000000099", UserID: fixture.tenant.UserID, Role: domain.RoleOwner}
	if _, err := fixture.service.OpenMessage(context.Background(), foreign, result.MessageID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant raw error = %v", err)
	}
	if _, err := fixture.service.OpenAttachment(context.Background(), foreign, queued.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant attachment error = %v", err)
	}
	finance := fixture.tenant
	finance.Role = domain.RoleFinance
	if _, err := fixture.service.ListSources(context.Background(), finance); err != nil {
		t.Fatalf("finance source read = %v", err)
	}
	for _, role := range []domain.Role{domain.RoleReviewer, domain.RoleViewer} {
		denied := fixture.tenant
		denied.Role = role
		if _, err := fixture.service.ListSources(context.Background(), denied); !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("%s source read error = %v", role, err)
		}
	}
	if _, err := fixture.service.Register(context.Background(), RegisterInput{
		Tenant: finance, Registration: validEmailRegistration(),
		IdempotencyKey: "finance-register", RequestID: "finance-register-request",
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("finance registration error = %v", err)
	}

	assertArchiveAuditIsSafe(t, fixture.store, fixture.tenant.TenantID)
	assertEmailRowsImmutable(t, fixture.store, result.MessageID, queued.ID, source.ID)

	deletion := documents.NewDeletionService(
		fixture.store, fixture.objects, fixture.store, system.IDGenerator{}, emailTestClock{now: fixture.now.Add(2 * time.Hour)},
	)
	if err := deletion.Delete(context.Background(), fixture.tenant, queued.DocumentID, "delete-email-document"); err != nil {
		t.Fatal(err)
	}
	page, err := fixture.service.ListMessages(context.Background(), fixture.tenant, source.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range page.Items {
		for _, attachment := range message.Attachments {
			if attachment.OriginalName == "invoice.png" &&
				(attachment.DocumentID != "" || attachment.ProcessingStatus != domain.EmailAttachmentArchivedOnly ||
					attachment.SafeReasonCode != "document_deleted") {
				t.Fatalf("detached attachment = %#v", attachment)
			}
		}
	}
	preserved, err := fixture.service.OpenAttachment(context.Background(), fixture.tenant, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	preservedPNG, err := ioReadAllAndClose(preserved.Body)
	if err != nil || !bytes.Equal(preservedPNG, png) {
		t.Fatalf("email-owned object was deleted: %v", err)
	}
}

func TestEmailArchivePaginationConcurrencyAndBlockedMessages(t *testing.T) {
	fixture := newEmailIntegrationFixture(t)
	source := fixture.registerSource(t, "email-source-pagination")
	base := fixture.now.Add(time.Hour)
	for index := 0; index < 3; index++ {
		if _, err := fixture.service.Archive(context.Background(), ArchiveInput{
			TenantID: fixture.tenant.TenantID, EmailSourceID: source.ID,
			ExternalMessageKey: strings.Repeat(string(rune('c'+index)), 64), ReceivedAt: base.Add(time.Duration(index) * time.Minute),
			Raw: bytes.NewReader(syntheticPlainEmail(fmt.Sprintf("合成-%d", index))), RequestID: fmt.Sprintf("page-%d", index),
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := fixture.service.ListMessages(context.Background(), fixture.tenant, source.ID, "", 2)
	if err != nil || len(first.Items) != 2 || first.NextCursor == "" || first.Items[0].Subject != "合成-2" {
		t.Fatalf("first page = %#v, error=%v", first, err)
	}
	second, err := fixture.service.ListMessages(context.Background(), fixture.tenant, source.ID, first.NextCursor, 2)
	if err != nil || len(second.Items) != 1 || second.NextCursor != "" || second.Items[0].Subject != "合成-0" {
		t.Fatalf("second page = %#v, error=%v", second, err)
	}
	invalidCursor := base64.RawURLEncoding.EncodeToString([]byte(`{"version":"email-message-cursor/1","received_at":"2026-08-31T00:00:00Z","id":"message"}{}`))
	if _, err := fixture.service.ListMessages(context.Background(), fixture.tenant, source.ID, invalidCursor, 2); err == nil {
		t.Fatal("cursor with trailing JSON was accepted")
	}
	if _, err := fixture.service.ListMessages(context.Background(), fixture.tenant, source.ID, "", 101); err == nil {
		t.Fatal("oversized page limit was accepted")
	}

	blocked, err := fixture.service.Archive(context.Background(), ArchiveInput{
		TenantID: fixture.tenant.TenantID, EmailSourceID: source.ID,
		ExternalMessageKey: strings.Repeat("f", 64), ReceivedAt: base.Add(4 * time.Minute),
		Raw: bytes.NewReader([]byte("not a mail header")), RequestID: "blocked-message",
	})
	if err != nil || blocked.Status != domain.EmailMessageBlocked || blocked.SafeErrorCode != "email_mime_invalid" || len(blocked.Attachments) != 0 {
		t.Fatalf("blocked archive = %#v, error=%v", blocked, err)
	}

	concurrentRaw := syntheticPlainEmail("并发合成邮件")
	key := strings.Repeat("9", 64)
	results := make(chan ports.EmailArchiveResult, 8)
	errorsChannel := make(chan error, 8)
	var group sync.WaitGroup
	for index := 0; index < 8; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			result, err := fixture.service.Archive(context.Background(), ArchiveInput{
				TenantID: fixture.tenant.TenantID, EmailSourceID: source.ID,
				ExternalMessageKey: key, ReceivedAt: base.Add(5 * time.Minute),
				Raw: bytes.NewReader(concurrentRaw), RequestID: fmt.Sprintf("concurrent-%d", index),
			})
			results <- result
			errorsChannel <- err
		}()
	}
	group.Wait()
	close(results)
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatalf("concurrent archive error = %v", err)
		}
	}
	messageIDs := map[string]struct{}{}
	for result := range results {
		messageIDs[result.MessageID] = struct{}{}
	}
	if len(messageIDs) != 1 {
		t.Fatalf("concurrent message ids = %#v", messageIDs)
	}
	var count int
	if err := fixture.store.DB().QueryRow(`
		SELECT count(*) FROM email_messages
		WHERE tenant_id = ? AND email_source_id = ? AND external_message_key = ?
	`, fixture.tenant.TenantID, source.ID, key).Scan(&count); err != nil || count != 1 {
		t.Fatalf("concurrent message count = %d, error=%v", count, err)
	}
}

func TestEmailArchiveCompensatesDatabaseAndObjectFailures(t *testing.T) {
	t.Run("database", func(t *testing.T) {
		fixture := newEmailIntegrationFixture(t)
		source := fixture.registerSource(t, "email-source-database-failure")
		if _, err := fixture.store.DB().Exec(`
			CREATE TRIGGER fail_email_message_insert
			BEFORE INSERT ON email_messages
			BEGIN SELECT RAISE(ABORT, 'synthetic_email_database_failure'); END
		`); err != nil {
			t.Fatal(err)
		}
		_, err := fixture.service.Archive(context.Background(), ArchiveInput{
			TenantID: fixture.tenant.TenantID, EmailSourceID: source.ID,
			ExternalMessageKey: strings.Repeat("d", 64), ReceivedAt: fixture.now.Add(time.Hour),
			Raw: bytes.NewReader(syntheticPlainEmail("数据库失败")), RequestID: "database-failure",
		})
		if err == nil {
			t.Fatal("database failure was ignored")
		}
		assertNoEmailArchiveResidue(t, fixture, source.ID)
	})

	t.Run("object", func(t *testing.T) {
		fixture := newEmailIntegrationFixture(t)
		source := fixture.registerSource(t, "email-source-object-failure")
		failingObjects := &failCommitObjectStore{ObjectStore: fixture.objects, failAt: 2}
		service := NewService(
			fixture.store, fixture.store, failingObjects, fixture.inspector,
			emailmime.Parser{}, system.IDGenerator{}, emailTestClock{now: fixture.now},
		)
		png, err := base64.StdEncoding.DecodeString(syntheticOnePixelPNG)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Archive(context.Background(), ArchiveInput{
			TenantID: fixture.tenant.TenantID, EmailSourceID: source.ID,
			ExternalMessageKey: strings.Repeat("e", 64), ReceivedAt: fixture.now.Add(time.Hour),
			Raw: bytes.NewReader(syntheticImageEmail("对象失败", png)), RequestID: "object-failure",
		})
		if err == nil || !strings.Contains(err.Error(), "commit email archive object") {
			t.Fatalf("object failure = %v", err)
		}
		assertNoEmailArchiveResidue(t, fixture, source.ID)
	})
}

func TestReadEmailRawEnforcesExactSizeBoundary(t *testing.T) {
	exact := bytes.Repeat([]byte{'x'}, int(domain.MaxEmailMessageBytes))
	read, err := readEmailRaw(bytes.NewReader(exact))
	if err != nil || int64(len(read)) != domain.MaxEmailMessageBytes {
		t.Fatalf("exact boundary = %d, error=%v", len(read), err)
	}
	tooLarge := append(exact, 'x')
	_, err = readEmailRaw(bytes.NewReader(tooLarge))
	var rule *domain.RuleError
	if !errors.As(err, &rule) || rule.Code != "email_message_too_large" || !errors.Is(err, domain.ErrPayloadTooLarge) {
		t.Fatalf("oversize error = %v", err)
	}
}

type emailIntegrationFixture struct {
	store      *sqliteadapter.Store
	objects    *localstorage.Store
	inspector  localstorage.Inspector
	service    Service
	tenant     domain.TenantContext
	now        time.Time
	objectRoot string
}

func newEmailIntegrationFixture(t *testing.T) emailIntegrationFixture {
	t.Helper()
	root := t.TempDir()
	store, err := sqliteadapter.Open(context.Background(), sqliteadapter.Config{
		DatabasePath: filepath.Join(root, "email.sqlite"), MigrationsDir: emailMigrationsDir(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, 8, 31, 8, 0, 0, 0, time.UTC)
	owner := ports.BootstrapOwner{
		UserID: "00000000-0000-4000-8000-000000000301", TenantID: "00000000-0000-4000-8000-000000000302",
		Email: "owner@example.invalid", PasswordHash: "synthetic-only", DisplayName: "Owner",
		TenantName: "Synthetic", DefaultCurrency: domain.CurrencyCNY, Timezone: "UTC", CreatedAt: now,
	}
	if err := store.BootstrapOwner(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	objectRoot := filepath.Join(root, "objects")
	objects, err := localstorage.New(objectRoot)
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := localstorage.NewInspector(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(
		store, store, objects, inspector, emailmime.Parser{}, system.IDGenerator{}, emailTestClock{now: now},
	)
	return emailIntegrationFixture{
		store: store, objects: objects, inspector: inspector, service: service,
		tenant: domain.TenantContext{TenantID: owner.TenantID, UserID: owner.UserID, Role: domain.RoleOwner},
		now:    now, objectRoot: objectRoot,
	}
}

func (f emailIntegrationFixture) registerSource(t *testing.T, idempotencyKey string) ports.EmailSource {
	t.Helper()
	result, err := f.service.Register(context.Background(), RegisterInput{
		Tenant: f.tenant, Registration: validEmailRegistration(),
		IdempotencyKey: idempotencyKey, RequestID: idempotencyKey + "-request",
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.Source
}

func validEmailRegistration() domain.EmailSourceRegistration {
	return domain.EmailSourceRegistration{
		DisplayName: " 合成财务邮箱 ", MailboxAddress: "finance@example.invalid",
		IMAPHost: "IMAP.EXAMPLE.INVALID", IMAPPort: 993,
		TransportSecurity: domain.EmailTransportImplicitTLS,
	}
}

func syntheticMixedEmail(png []byte) []byte {
	return []byte(strings.Join([]string{
		"From: Synthetic Sender <sender@example.invalid>",
		"To: finance@example.invalid",
		"Date: Sun, 31 Aug 2026 10:20:30 +0800",
		"Subject: =?UTF-8?B?5ZCI5oiQ6LSm5Y2V?=",
		"Content-Type: multipart/mixed; boundary=synthetic",
		"",
		"--synthetic",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<img src=https://remote.invalid/private><script>ignored()</script>",
		"--synthetic",
		"Content-Type: image/png; name=invoice.png",
		"Content-Disposition: attachment; filename=invoice.png",
		"Content-Transfer-Encoding: base64",
		"",
		base64.StdEncoding.EncodeToString(png),
		"--synthetic",
		"Content-Type: text/plain; name=notes.txt",
		"Content-Disposition: attachment; filename=notes.txt",
		"",
		"synthetic note",
		"--synthetic",
		"Content-Type: image/png; name=forged.png",
		"Content-Disposition: attachment; filename=forged.png",
		"",
		"not a png",
		"--synthetic",
		"Content-Type: text/plain; name=empty.txt",
		"Content-Disposition: attachment; filename=empty.txt",
		"Content-Transfer-Encoding: base64",
		"",
		"",
		"--synthetic--",
		"",
	}, "\r\n"))
}

func syntheticImageEmail(subject string, png []byte) []byte {
	return []byte(strings.Join([]string{
		"From: sender@example.invalid",
		"Subject: " + subject,
		"Content-Type: image/png; name=invoice.png",
		"Content-Disposition: attachment; filename=invoice.png",
		"Content-Transfer-Encoding: base64",
		"",
		base64.StdEncoding.EncodeToString(png),
		"",
	}, "\r\n"))
}

func syntheticPlainEmail(subject string) []byte {
	return []byte("From: sender@example.invalid\r\nSubject: " + subject + "\r\nContent-Type: text/plain\r\n\r\nsynthetic body")
}

func findAttachmentByName(t *testing.T, items []ports.EmailAttachment, name string) ports.EmailAttachment {
	t.Helper()
	for _, item := range items {
		if item.OriginalName == name {
			return item
		}
	}
	t.Fatalf("attachment %q not found in %#v", name, items)
	return ports.EmailAttachment{}
}

func ioReadAllAndClose(reader interface {
	Read([]byte) (int, error)
	Close() error
}) ([]byte, error) {
	defer reader.Close()
	return io.ReadAll(reader)
}

func assertArchiveAuditIsSafe(t *testing.T, store *sqliteadapter.Store, tenantID string) {
	t.Helper()
	rows, err := store.DB().Query(`
		SELECT safe_metadata_json FROM audit_events
		WHERE tenant_id = ? AND action = 'email_message_archived'
	`, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var metadata string
		if err := rows.Scan(&metadata); err != nil {
			t.Fatal(err)
		}
		count++
		for _, privateValue := range []string{"合成账单", "sender@example.invalid", "invoice.png", "synthetic note"} {
			if strings.Contains(metadata, privateValue) {
				t.Fatalf("audit metadata exposed private value %q: %s", privateValue, metadata)
			}
		}
	}
	if err := rows.Err(); err != nil || count != 2 {
		t.Fatalf("archive audits = %d, error=%v", count, err)
	}
}

func assertEmailRowsImmutable(t *testing.T, store *sqliteadapter.Store, messageID, attachmentID, sourceID string) {
	t.Helper()
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{query: "UPDATE email_messages SET subject = 'changed' WHERE id = ?", args: []any{messageID}},
		{query: "UPDATE email_attachments SET original_name = 'changed.txt' WHERE id = ?", args: []any{attachmentID}},
		{query: "UPDATE email_sources SET imap_host_normalized = 'changed.invalid' WHERE id = ?", args: []any{sourceID}},
	} {
		if _, err := store.DB().Exec(statement.query, statement.args...); err == nil {
			t.Fatalf("immutable row accepted mutation: %s", statement.query)
		}
	}
}

func assertNoEmailArchiveResidue(t *testing.T, fixture emailIntegrationFixture, sourceID string) {
	t.Helper()
	var messages, attachments, documentsCount, jobs, archiveAudits int
	if err := fixture.store.DB().QueryRow(`
		SELECT
			(SELECT count(*) FROM email_messages WHERE tenant_id = ? AND email_source_id = ?),
			(SELECT count(*) FROM email_attachments WHERE tenant_id = ?),
			(SELECT count(*) FROM documents WHERE tenant_id = ?),
			(SELECT count(*) FROM processing_jobs WHERE tenant_id = ?),
			(SELECT count(*) FROM audit_events WHERE tenant_id = ? AND action = 'email_message_archived')
	`, fixture.tenant.TenantID, sourceID, fixture.tenant.TenantID, fixture.tenant.TenantID,
		fixture.tenant.TenantID, fixture.tenant.TenantID).Scan(
		&messages, &attachments, &documentsCount, &jobs, &archiveAudits,
	); err != nil {
		t.Fatal(err)
	}
	if messages != 0 || attachments != 0 || documentsCount != 0 || jobs != 0 || archiveAudits != 0 {
		t.Fatalf("archive residue = messages:%d attachments:%d documents:%d jobs:%d audits:%d",
			messages, attachments, documentsCount, jobs, archiveAudits)
	}
	sources, err := fixture.service.ListSources(context.Background(), fixture.tenant)
	if err != nil || len(sources) != 1 || sources[0].Status != domain.EmailSourcePendingConnection || sources[0].LastArchivedAt != nil {
		t.Fatalf("source after failure = %#v, error=%v", sources, err)
	}
	regularFiles := 0
	if err := filepath.WalkDir(fixture.objectRoot, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			regularFiles++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if regularFiles != 0 {
		t.Fatalf("object store retained %d files", regularFiles)
	}
}

type failCommitObjectStore struct {
	ports.ObjectStore
	mu      sync.Mutex
	commits int
	failAt  int
}

func (s *failCommitObjectStore) Commit(ctx context.Context, staged ports.StagedObject, storageKey string) error {
	s.mu.Lock()
	s.commits++
	current := s.commits
	s.mu.Unlock()
	if current == s.failAt {
		return errors.New("synthetic object commit failure")
	}
	return s.ObjectStore.Commit(ctx, staged, storageKey)
}

type emailTestClock struct{ now time.Time }

func (c emailTestClock) Now() time.Time { return c.now }

func emailMigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../../infra/migrations"))
}
