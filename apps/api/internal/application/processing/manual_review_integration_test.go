package processing

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func TestFailedWorkerCanEnterManualReviewWithoutCallingProviderAgain(t *testing.T) {
	for _, missing := range []bool{true, false} {
		f := newWorkerFixture(t)
		ctx := context.Background()
		calls := 0
		if missing {
			if _, err := f.store.DB().Exec(`UPDATE provider_configs SET active = FALSE`); err != nil {
				t.Fatal(err)
			}
		}
		worker := f.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
			calls++
			return ports.BillExtractionResult{}, &ports.ProviderCallError{Code: "provider_unauthorized", SafeMessage: "合成 Provider 失败", Retryable: false}
		}})
		if err := worker.ProcessOne(ctx, f.job); err != nil {
			t.Fatal(err)
		}
		before, err := f.store.GetJob(ctx, f.tenant.TenantID, f.job.ID)
		if err != nil {
			t.Fatal(err)
		}
		pages, err := f.store.GetDocumentPages(ctx, f.tenant.TenantID, f.job.DocumentID)
		if err != nil {
			t.Fatal(err)
		}
		var runsBefore string
		if err := f.store.DB().QueryRow(`SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id), '[]'::jsonb)::text FROM ai_runs a`).Scan(&runsBefore); err != nil {
			t.Fatal(err)
		}
		service := reviews.NewService(f.store, f.store, system.IDGenerator{}, workerClock{now: f.now.Add(2 * time.Minute)}).WithManualEntry(f.store, f.normalizer, f.objects)
		if _, err := service.StartManualReview(ctx, f.tenant, f.job.ID, reviews.ManualReviewInput{ExpectedJobVersion: before.Version, DocumentType: domain.DocumentPayment, Reason: "人工接管失败", IdempotencyKey: "worker-manual-root"}); err != nil {
			t.Fatal(err)
		}
		after, err := f.store.GetJob(ctx, f.tenant.TenantID, f.job.ID)
		if err != nil {
			t.Fatal(err)
		}
		var runsAfter string
		if err := f.store.DB().QueryRow(`SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id), '[]'::jsonb)::text FROM ai_runs a`).Scan(&runsAfter); err != nil {
			t.Fatal(err)
		}
		if before.ErrorCode != after.ErrorCode || before.AttemptCount != after.AttemptCount || runsBefore != runsAfter || (missing && calls != 0) || (!missing && calls != 1) {
			t.Fatal("manual entry changed failed AI history or called provider")
		}
		if !missing {
			current, err := f.store.GetDocumentPages(ctx, f.tenant.TenantID, f.job.DocumentID)
			if err != nil || !reflect.DeepEqual(pages, current) {
				t.Fatal("manual entry did not reuse prepared pages")
			}
		}
	}
}

func TestLateWorkerCannotOverwriteManualReviewAfterLeaseRecovery(t *testing.T) {
	for _, lateSuccess := range []bool{true, false} {
		f := newWorkerFixture(t)
		ctx := context.Background()
		entered, release := make(chan struct{}), make(chan struct{})
		old := f.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
			close(entered)
			<-release
			if lateSuccess {
				return ports.BillExtractionResult{Envelope: paymentExtractionEnvelope(), ResponseHash: "synthetic-late", Latency: time.Millisecond}, nil
			}
			return ports.BillExtractionResult{}, &ports.ProviderCallError{Code: "provider_unauthorized", SafeMessage: "合成迟到失败", Retryable: false}
		}})
		finished := make(chan error, 1)
		go func() { finished <- old.ProcessOne(ctx, f.job) }()
		<-entered
		recovered, err := f.store.LeaseNextJob(ctx, "manual-recovery-worker", f.now.Add(166*time.Second), f.now.Add(331*time.Second))
		if err != nil {
			close(release)
			<-finished
			t.Fatal(err)
		}
		fresh := f.worker(t, fakeExtractor{execute: func(context.Context) (ports.BillExtractionResult, error) {
			return ports.BillExtractionResult{}, &ports.ProviderCallError{Code: "provider_unauthorized", SafeMessage: "合成恢复失败", Retryable: false}
		}})
		if err := fresh.ProcessOne(ctx, recovered); err != nil {
			close(release)
			<-finished
			t.Fatal(err)
		}
		failed, err := f.store.GetJob(ctx, f.tenant.TenantID, f.job.ID)
		if err != nil {
			close(release)
			<-finished
			t.Fatal(err)
		}
		service := reviews.NewService(f.store, f.store, system.IDGenerator{}, workerClock{now: f.now.Add(3 * time.Minute)}).WithManualEntry(f.store, f.normalizer, f.objects)
		root, err := service.StartManualReview(ctx, f.tenant, f.job.ID, reviews.ManualReviewInput{ExpectedJobVersion: failed.Version, DocumentType: domain.DocumentPayment, Reason: "租约恢复后人工接管", IdempotencyKey: "manual-after-recovery"})
		if err != nil {
			close(release)
			<-finished
			t.Fatal(err)
		}
		before, err := service.Get(ctx, f.tenant, f.job.ID)
		if err != nil {
			close(release)
			<-finished
			t.Fatal(err)
		}
		close(release)
		if err := <-finished; err == nil {
			t.Fatal("late worker reported accepted result")
		}
		after, err := service.Get(ctx, f.tenant, f.job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.ClaimSetID != root.ClaimSetID || !reflect.DeepEqual(before, after) {
			t.Fatal("late worker changed manual review")
		}
	}
}
