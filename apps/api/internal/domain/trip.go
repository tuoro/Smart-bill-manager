package domain

const (
	TripAttributionRuleVersion   = "trip-attribution/1"
	TripAttributionViewAll       = "all"
	TripAttributionViewSuggested = "suggested"
	TripAttributionViewAssigned  = "assigned"
)

func ValidTripAttributionView(value string) bool {
	return value == TripAttributionViewAll || value == TripAttributionViewSuggested || value == TripAttributionViewAssigned
}

func ValidTripAssignmentFactType(value DocumentType) bool {
	return value == DocumentPayment || value == DocumentInvoice
}
