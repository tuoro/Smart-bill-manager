package httpapi

import (
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

func (s *Server) listPaymentsHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	items, err := s.facts.ListPayments(request.Context(), tenantContext(principal))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": paymentResponses(items)})
}

func (s *Server) listInvoicesHandler(response http.ResponseWriter, request *http.Request) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return
	}
	items, err := s.facts.ListInvoices(request.Context(), tenantContext(principal))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": invoiceResponses(items)})
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

func paymentResponses(items []ports.Payment) []ports.Payment {
	if items == nil {
		return []ports.Payment{}
	}
	return items
}

func invoiceResponses(items []ports.Invoice) []ports.Invoice {
	if items == nil {
		return []ports.Invoice{}
	}
	return items
}
