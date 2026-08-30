package domain

type JobStatus string

const (
	JobQueued          JobStatus = "queued"
	JobProcessing      JobStatus = "processing"
	JobNeedsReview     JobStatus = "needs_review"
	JobBlocked         JobStatus = "blocked"
	JobFailed          JobStatus = "failed"
	JobCancelRequested JobStatus = "cancel_requested"
	JobCancelled       JobStatus = "cancelled"
	JobCompleted       JobStatus = "completed"
	JobRejected        JobStatus = "rejected"
)

func (s JobStatus) CanCancel(hasFact bool) bool {
	if hasFact {
		return false
	}
	switch s {
	case JobQueued, JobProcessing, JobNeedsReview, JobBlocked:
		return true
	default:
		return false
	}
}

func (s JobStatus) Valid() bool {
	switch s {
	case JobQueued, JobProcessing, JobNeedsReview, JobBlocked, JobFailed,
		JobCancelRequested, JobCancelled, JobCompleted, JobRejected:
		return true
	default:
		return false
	}
}

func (s JobStatus) CanRetry(hasClaimSet bool) bool {
	return s == JobFailed && !hasClaimSet
}

func (s JobStatus) Terminal() bool {
	switch s {
	case JobCancelled, JobCompleted, JobRejected:
		return true
	default:
		return false
	}
}
