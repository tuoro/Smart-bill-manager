package reviews

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	postgres "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

type manualDBFixture struct {
	store      *postgres.Store
	objects    *localstorage.Store
	normalizer localstorage.Normalizer
	service    Service
	tenant     domain.TenantContext
	job        ports.JobSummary
	now        time.Time
}

func TestManualReviewPostgreSQLSourceIdentityAndInputFailures(t *testing.T) {
	f := newManualDBFixture(t)
	ctx := context.Background()
	// 精确检查约束名，避免测试被无关唯一键错误误判为通过。
	for _, test := range []struct {
		actor, reason, key string
		constraint         string
	}{
		{"", "合成人工理由", "manual-invalid-source", "claim_sets_origin_check"},
		{f.tenant.UserID, "", "manual-invalid-reason", "claim_sets_manual_identity_check"},
		{f.tenant.UserID, "合成人工理由", "x", "claim_sets_manual_identity_check"},
	} {
		_, err := f.store.DB().Exec(`INSERT INTO claim_sets (id,tenant_id,document_id,revised_by_user_id,document_type,status,revision,optimistic_version,manual_reason,manual_idempotency_key,manual_request_hash,created_at) VALUES (?,?,?,NULLIF(?,''),'payment','draft',1,1,?,?,?,?)`, mustID(t, system.IDGenerator{}), f.tenant.TenantID, f.job.DocumentID, test.actor, test.reason, test.key, strings.Repeat("a", 64), f.now)
		var failure *pgconn.PgError
		if !errors.As(err, &failure) || failure.ConstraintName != test.constraint {
			t.Fatalf("wrong source rejection: %v", err)
		}
	}
	input := f.input(domain.DocumentPayment, "manual-source-root")
	if _, err := f.service.StartManualReview(ctx, domain.TenantContext{TenantID: f.tenant.TenantID, UserID: f.tenant.UserID, Role: domain.RoleViewer}, f.job.ID, input); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("viewer accepted")
	}
	stale := input
	stale.ExpectedJobVersion++
	if _, err := f.service.StartManualReview(ctx, f.tenant, f.job.ID, stale); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatal("stale version accepted")
	}
	root, err := f.service.StartManualReview(ctx, f.tenant, f.job.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*ManualReviewInput){func(i *ManualReviewInput) { i.DocumentType = domain.DocumentInvoice }, func(i *ManualReviewInput) { i.ExpectedJobVersion++ }, func(i *ManualReviewInput) { i.Reason = "不同理由" }} {
		changed := input
		mutate(&changed)
		if _, err := f.service.StartManualReview(ctx, f.tenant, f.job.ID, changed); !errors.Is(err, domain.ErrConflict) {
			t.Fatal("changed idempotent payload accepted")
		}
	}
	if _, err := f.store.DB().Exec(`UPDATE claim_sets SET manual_reason = 'changed' WHERE id = ?`, root.ClaimSetID); err == nil {
		t.Fatal("root provenance was mutable")
	}
	_, err = f.store.DB().Exec(`INSERT INTO claim_sets (id,tenant_id,document_id,revised_by_user_id,document_type,status,revision,optimistic_version,supersedes_claim_set_id,created_at) VALUES (?,?,?,?,'payment','draft',3,1,?,?)`, mustID(t, system.IDGenerator{}), f.tenant.TenantID, f.job.DocumentID, f.tenant.UserID, root.ClaimSetID, f.now)
	var failure *pgconn.PgError
	if !errors.As(err, &failure) || failure.Message != "claim_source_mismatch" {
		t.Fatal("skipped parent revision accepted")
	}
}

func TestManualReviewPostgreSQLRejectsMissingAndChangedOriginalWithoutWrites(t *testing.T) {
	for _, missing := range []bool{true, false} {
		f := newManualDBFixture(t)
		ctx := context.Background()
		document, err := f.store.GetDocument(ctx, f.tenant.TenantID, f.job.DocumentID)
		if err != nil {
			t.Fatal(err)
		}
		if missing {
			if err := f.objects.Delete(ctx, document.StorageKey); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := f.store.DB().Exec(`UPDATE documents SET sha256 = ? WHERE id = ?`, strings.Repeat("0", 64), document.ID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.service.StartManualReview(ctx, f.tenant, f.job.ID, f.input(domain.DocumentPayment, "manual-bad-source")); !errors.Is(err, domain.ErrConflict) {
			t.Fatal("bad original accepted")
		}
		var count int
		if err := f.store.DB().QueryRow(`SELECT (SELECT count(*) FROM claim_sets)+(SELECT count(*) FROM document_pages)+(SELECT count(*) FROM audit_events)`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("bad source created state")
		}
	}
}

func newManualDBFixture(t *testing.T) manualDBFixture {
	t.Helper()
	ctx := context.Background()
	store := postgresqltest.Open(t)
	ids := system.IDGenerator{}
	now := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	tenant := domain.TenantContext{TenantID: mustID(t, ids), UserID: mustID(t, ids), Role: domain.RoleOwner}
	if err := store.BootstrapOwner(ctx, ports.BootstrapOwner{TenantID: tenant.TenantID, UserID: tenant.UserID, Email: "manual@example.test", PasswordHash: "synthetic-nonlogin-hash", DisplayName: "合成用户", TenantName: "合成租户", DefaultCurrency: domain.CurrencyCNY, Timezone: "Asia/Shanghai", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	objects, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	inspector, err := localstorage.NewInspector(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	normalizer, err := localstorage.NewNormalizer(objects, "/bin/false")
	if err != nil {
		t.Fatal(err)
	}
	content, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	upload := documents.NewUploadService(objects, inspector, store, ids, fixedClock{now: now})
	created, err := upload.Execute(ctx, documents.UploadInput{Tenant: tenant, Name: "synthetic-manual.png", MIME: "image/png", Source: bytes.NewReader(content)})
	if err != nil {
		t.Fatal(err)
	}
	leased, err := store.LeaseNextJob(ctx, "manual-fixture-worker", now, now.Add(165*time.Second))
	if err != nil || leased.ID != created.JobID {
		t.Fatal("fixture failed to lease uploaded document")
	}
	if err := store.WithinTransaction(ctx, func(tx ports.Transaction) error {
		return tx.MarkJobFailed(ctx, tenant.TenantID, leased.ID, "provider_config_missing", "尚未配置模型", now)
	}); err != nil {
		t.Fatal(err)
	}
	job, err := store.GetJob(ctx, tenant.TenantID, leased.ID)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, store, ids, fixedClock{now: now.Add(time.Minute)}).WithManualEntry(store, normalizer, objects)
	return manualDBFixture{store, objects, normalizer, service, tenant, job, now}
}

func (f manualDBFixture) input(kind domain.DocumentType, key string) ManualReviewInput {
	return ManualReviewInput{DocumentType: kind, ExpectedJobVersion: f.job.Version, Reason: "按原件人工核对", IdempotencyKey: key}
}

func TestManualReviewPostgreSQLThreeTypesPreserveUserSourceThroughConfirmation(t *testing.T) {
	for _, envelope := range []domain.ClaimEnvelope{paymentEnvelope(), invoiceWithItemsEnvelope("SYN-MANUAL-001"), tripEnvelope("合成起点", "合成终点", "2026-08-26", "2026-08-28")} {
		t.Run(string(envelope.DocumentType), func(t *testing.T) {
			f := newManualDBFixture(t)
			ctx := context.Background()
			input := f.input(domain.DocumentType(envelope.DocumentType), "manual-root-request")
			root, err := f.service.StartManualReview(ctx, f.tenant, f.job.ID, input)
			if err != nil {
				t.Fatal(err)
			}
			current, err := f.service.Get(ctx, f.tenant, f.job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.OriginAiRunID != "" || current.Revision != 1 || current.Status != domain.ClaimBlocked || current.Job.ErrorCode != f.job.ErrorCode || current.Job.AttemptCount != f.job.AttemptCount {
				t.Fatal("manual root lost failure/source identity")
			}
			if _, err := f.service.Revise(ctx, f.tenant, f.job.ID, revisionInputFrom(current)); err != nil {
				t.Fatal(err)
			}
			current, err = f.service.Get(ctx, f.tenant, f.job.ID)
			if err != nil || current.Status != domain.ClaimBlocked || current.Revision != 2 {
				t.Fatal("partial manual draft did not survive reload")
			}
			revision := RevisionInput{ExpectedRevision: current.Revision, ExpectedOptimisticVersion: current.OptimisticVersion, DocumentType: domain.DocumentType(envelope.DocumentType)}
			for _, field := range envelope.Fields {
				entry := RevisionFieldInput{Path: field.Path, ValueType: field.ValueType, Presence: field.Presence, Value: field.Value}
				for _, evidence := range field.Evidence {
					entry.ManualEvidence = append(entry.ManualEvidence, domain.ManualEvidenceInput{Page: evidence.Page, Quote: evidence.Quote})
				}
				revision.Fields = append(revision.Fields, entry)
			}
			current, err = f.service.Revise(ctx, f.tenant, f.job.ID, revision)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status != domain.ClaimReadyForReview || current.Revision != 3 || current.OriginAiRunID != "" {
				t.Fatal("complete manual revision is not reviewable")
			}
			confirmation := ConfirmInput{ExpectedRevision: 3, IdempotencyKey: "manual-confirm-request", RequestID: "manual-confirm-audit"}
			if envelope.DocumentType != string(domain.DocumentTrip) {
				confirmation.AssociationMode = AssociationNoCandidate
			}
			result, err := f.service.Confirm(ctx, f.tenant, f.job.ID, confirmation)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := f.service.Confirm(ctx, f.tenant, f.job.ID, confirmation)
			if err != nil || !replay.Replayed || replay.FactID != result.FactID {
				t.Fatal("manual confirmation replay changed fact")
			}
			rootReplay, err := f.service.StartManualReview(ctx, f.tenant, f.job.ID, input)
			if err != nil || !rootReplay.Replayed || rootReplay.ClaimSetID != root.ClaimSetID {
				t.Fatal("root replay after confirmation changed identity")
			}
			var runs, roots, traced, manualAudits, containers int
			if err := f.store.DB().QueryRowContext(ctx, `SELECT (SELECT count(*) FROM ai_runs),
			 (SELECT count(*) FROM claim_sets WHERE origin_ai_run_id IS NULL AND revised_by_user_id = ?),
			 (SELECT count(*) FROM fact_field_origins o JOIN field_claims f ON f.tenant_id = o.tenant_id AND f.id = o.field_claim_id JOIN claim_sets c ON c.tenant_id = f.tenant_id AND c.id = f.claim_set_id JOIN review_decisions r ON r.id = o.review_decision_id AND r.tenant_id = o.tenant_id WHERE c.origin_ai_run_id IS NULL AND f.source = 'user' AND r.action = 'confirm'),
			 (SELECT count(*) FROM audit_events WHERE action = 'manual_review_started'), (SELECT count(*) FROM trips)`, f.tenant.UserID).Scan(&runs, &roots, &traced, &manualAudits, &containers); err != nil {
				t.Fatal(err)
			}
			if runs != 0 || roots != 3 || traced == 0 || manualAudits != 1 || containers != 0 {
				t.Fatal("manual fact lacks a unique user-review chain or fabricated AI/container")
			}
		})
	}
}

func TestManualReviewPostgreSQLCompetingRootsAndReplay(t *testing.T) {
	for _, sameKey := range []bool{true, false} {
		t.Run(fmt.Sprint(sameKey), func(t *testing.T) {
			f := newManualDBFixture(t)
			start := make(chan struct{})
			var wait sync.WaitGroup
			results := make([]ManualReviewResult, 2)
			errorsFound := make([]error, 2)
			for index := range 2 {
				wait.Add(1)
				go func() {
					defer wait.Done()
					<-start
					key := "manual-concurrent"
					if !sameKey {
						key += fmt.Sprint(index)
					}
					results[index], errorsFound[index] = f.service.StartManualReview(context.Background(), f.tenant, f.job.ID, f.input(domain.DocumentPayment, key))
				}()
			}
			close(start)
			wait.Wait()
			successes := 0
			for _, err := range errorsFound {
				if err == nil {
					successes++
				} else if !errors.Is(err, domain.ErrConflict) {
					t.Fatal(err)
				}
			}
			if sameKey && (successes != 2 || results[0].ClaimSetID != results[1].ClaimSetID || results[0].Replayed == results[1].Replayed) {
				t.Fatal("same request did not converge")
			}
			if !sameKey && successes != 1 {
				t.Fatal("competing roots were both accepted")
			}
			var claims, pages int
			if err := f.store.DB().QueryRow(`SELECT (SELECT count(*) FROM claim_sets), (SELECT count(*) FROM document_pages)`).Scan(&claims, &pages); err != nil {
				t.Fatal(err)
			}
			if claims != 1 || pages != 1 {
				t.Fatal("competition left duplicate state")
			}
		})
	}
}

func TestManualReviewPostgreSQLAuditFailureRollsBackPagesAndClaim(t *testing.T) {
	f := newManualDBFixture(t)
	ctx := context.Background()
	if _, err := f.store.DB().Exec(`CREATE FUNCTION fail_manual_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic_manual_failure'; END; $$; CREATE TRIGGER fail_manual_audit BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION fail_manual_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartManualReview(ctx, f.tenant, f.job.ID, f.input(domain.DocumentPayment, "manual-rollback")); err == nil {
		t.Fatal("audit failure accepted")
	}
	var claims, pages int
	if err := f.store.DB().QueryRow(`SELECT (SELECT count(*) FROM claim_sets), (SELECT count(*) FROM document_pages)`).Scan(&claims, &pages); err != nil {
		t.Fatal(err)
	}
	if claims != 0 || pages != 0 {
		t.Fatal("transaction did not roll back aggregate")
	}
	job, err := f.store.GetJob(ctx, f.tenant.TenantID, f.job.ID)
	if err != nil || job.Status != f.job.Status || job.Version != f.job.Version {
		t.Fatal("rollback changed failed job")
	}
	if _, err := f.store.DB().Exec(`DROP TRIGGER fail_manual_audit ON audit_events`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.StartManualReview(ctx, f.tenant, f.job.ID, f.input(domain.DocumentPayment, "manual-after-rollback")); err != nil {
		t.Fatal(err)
	}
}

type manualDuringDeletion struct {
	*postgres.Store
	start func()
}

func (r manualDuringDeletion) PrepareUnconfirmedDocumentDeletion(ctx context.Context, tenantID, documentID string) (ports.DocumentDeletionPlan, error) {
	plan, err := r.Store.PrepareUnconfirmedDocumentDeletion(ctx, tenantID, documentID)
	if err == nil {
		r.start()
	}
	return plan, err
}

func TestManualReviewPostgreSQLStaleDeletionRestoresOriginalAndKeepsNewPages(t *testing.T) {
	f := newManualDBFixture(t)
	ctx := context.Background()
	repository := manualDuringDeletion{Store: f.store, start: func() {
		if _, err := f.service.StartManualReview(ctx, f.tenant, f.job.ID, f.input(domain.DocumentPayment, "manual-before-delete")); err != nil {
			t.Fatal(err)
		}
	}}
	deletion := documents.NewDeletionService(repository, f.objects, f.store, system.IDGenerator{}, fixedClock{now: f.now})
	if err := deletion.Delete(ctx, f.tenant, f.job.DocumentID, "manual-delete-race"); !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("stale deletion error = %v", err)
	}
	document, err := f.store.GetDocument(ctx, f.tenant.TenantID, f.job.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := f.objects.Open(ctx, document.StorageKey)
	if err != nil {
		t.Fatal("stale deletion did not restore original")
	}
	reader.Close()
	pages, err := f.store.GetDocumentPages(ctx, f.tenant.TenantID, f.job.DocumentID)
	if err != nil || len(pages) != 1 {
		t.Fatal("stale deletion removed new pages")
	}
	reader, err = f.objects.Open(ctx, pages[0].StorageKey)
	if err != nil {
		t.Fatal("new page object disappeared")
	}
	reader.Close()
	if _, err := f.service.Get(ctx, f.tenant, f.job.ID); err != nil {
		t.Fatal("stale deletion removed claim")
	}
}
