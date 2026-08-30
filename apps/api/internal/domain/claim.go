package domain

import "strings"

type DocumentType string

const (
	DocumentPayment DocumentType = "payment"
	DocumentInvoice DocumentType = "invoice"
	DocumentUnknown DocumentType = "unknown"
)

type ClaimStatus string

const (
	ClaimDraft          ClaimStatus = "draft"
	ClaimReadyForReview ClaimStatus = "ready_for_review"
	ClaimBlocked        ClaimStatus = "blocked"
	ClaimSuperseded     ClaimStatus = "superseded"
	ClaimConfirmed      ClaimStatus = "confirmed"
	ClaimRejected       ClaimStatus = "rejected"
	ClaimCancelled      ClaimStatus = "cancelled"
)

func (t DocumentType) Valid() bool {
	return t == DocumentPayment || t == DocumentInvoice || t == DocumentUnknown
}

func (s ClaimStatus) CanConfirm() bool {
	return s == ClaimReadyForReview
}

func StableItemPath(itemKey, field string) (string, error) {
	if !safePathToken(itemKey) || !safePathToken(field) {
		return "", ErrInvalidInput
	}
	return "items[" + itemKey + "]." + field, nil
}

func safePathToken(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			!strings.ContainsRune("_-", character) {
			return false
		}
	}
	return true
}
