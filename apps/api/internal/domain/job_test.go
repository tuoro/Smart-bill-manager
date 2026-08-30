package domain

import "testing"

func TestJobCancellationAndRetryBoundaries(t *testing.T) {
	t.Parallel()

	for _, status := range []JobStatus{JobQueued, JobProcessing, JobNeedsReview, JobBlocked} {
		if !status.CanCancel(false) {
			t.Errorf("%s must be cancellable before Fact", status)
		}
		if status.CanCancel(true) {
			t.Errorf("%s must not be cancellable after Fact", status)
		}
	}
	for _, status := range []JobStatus{JobFailed, JobCancelled, JobCompleted, JobRejected} {
		if status.CanCancel(false) {
			t.Errorf("%s must not be cancellable", status)
		}
	}
	if !JobFailed.CanRetry(false) {
		t.Fatal("failed Job without ClaimSet must be retryable")
	}
	if JobFailed.CanRetry(true) || JobBlocked.CanRetry(false) {
		t.Fatal("retry boundary accepted an invalid state")
	}
	for _, status := range []JobStatus{JobQueued, JobProcessing, JobNeedsReview, JobBlocked, JobFailed, JobCancelRequested, JobCancelled, JobCompleted, JobRejected} {
		if !status.Valid() {
			t.Fatalf("documented status %s is invalid", status)
		}
	}
	if JobStatus("unknown").Valid() {
		t.Fatal("unknown status accepted")
	}
	for _, status := range []JobStatus{JobCancelled, JobCompleted, JobRejected} {
		if !status.Terminal() {
			t.Fatalf("%s must be terminal", status)
		}
	}
	for _, status := range []JobStatus{JobQueued, JobProcessing, JobNeedsReview, JobBlocked, JobFailed, JobCancelRequested} {
		if status.Terminal() {
			t.Fatalf("%s must not be terminal", status)
		}
	}
}
