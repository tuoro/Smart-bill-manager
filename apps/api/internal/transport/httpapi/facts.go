package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Server) listPaymentsHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	input, err := parseFactQuery(request.URL.RawQuery)
	if err != nil {
		writeError(response, request, err)
		return
	}
	page, err := s.facts.ListPayments(request.Context(), tenantContext(principal), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (s *Server) listInvoicesHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	input, err := parseFactQuery(request.URL.RawQuery)
	if err != nil {
		writeError(response, request, err)
		return
	}
	page, err := s.facts.ListInvoices(request.Context(), tenantContext(principal), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (s *Server) listTripsHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	items, err := s.trips.List(request.Context(), tenantContext(principal))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": tripResponses(items)})
}

func (s *Server) deletePaymentHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	if err := s.facts.Delete(
		request.Context(), tenantContext(principal), domain.DocumentPayment,
		request.PathValue("payment_id"), requestIDFromRequest(request),
	); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteInvoiceHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	if err := s.facts.Delete(
		request.Context(), tenantContext(principal), domain.DocumentInvoice,
		request.PathValue("invoice_id"), requestIDFromRequest(request),
	); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteTripEvidenceHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	if err := s.facts.Delete(
		request.Context(), tenantContext(principal), domain.DocumentTrip,
		request.PathValue("evidence_id"), requestIDFromRequest(request),
	); err != nil {
		writeError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func tripResponses(items []ports.Trip) []ports.Trip {
	if items == nil {
		return []ports.Trip{}
	}
	return items
}

func parseFactQuery(raw string) (reviews.FactQueryInput, error) {
	input := reviews.FactQueryInput{Limit: 20}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return input, domain.ErrInvalidInput
	}
	for key, list := range values {
		if len(list) != 1 {
			return input, domain.ErrInvalidInput
		}
		value := list[0]
		switch key {
		case "cursor":
			input.Cursor = value
		case "limit":
			input.Limit, err = strconv.Atoi(value)
			if err != nil || input.Limit < 1 || input.Limit > 100 {
				return input, domain.ErrInvalidInput
			}
		case "date_from":
			input.Filter.DateFrom = value
		case "date_to":
			input.Filter.DateTo = value
		case "q":
			input.Filter.Query = value
		case "allocation_status":
			input.Filter.AllocationStatus = value
		default:
			return input, domain.ErrInvalidInput
		}
	}
	return input, nil
}

func (s *Server) getPaymentHandler(response http.ResponseWriter, request *http.Request) {
	s.getFactHandler(response, request, domain.DocumentPayment, request.PathValue("payment_id"))
}
func (s *Server) getInvoiceHandler(response http.ResponseWriter, request *http.Request) {
	s.getFactHandler(response, request, domain.DocumentInvoice, request.PathValue("invoice_id"))
}
func (s *Server) getFactHandler(response http.ResponseWriter, request *http.Request, kind domain.DocumentType, id string) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	if request.URL.RawQuery != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	detail, err := s.facts.Detail(request.Context(), tenantContext(principal), kind, id)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}
