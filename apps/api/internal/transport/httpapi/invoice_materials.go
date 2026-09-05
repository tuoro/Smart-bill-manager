package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/invoicematerials"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func materialTenant(response http.ResponseWriter, request *http.Request) (domain.TenantContext, bool) {
	principal, ok := principalFromRequest(request)
	if !ok {
		writeError(response, request, domain.ErrUnauthenticated)
		return domain.TenantContext{}, false
	}
	tenant := tenantContext(principal)
	if err := domain.RequireInvoiceMaterials(tenant); err != nil {
		writeError(response, request, err)
		return tenant, false
	}
	return tenant, true
}

func (s *Server) getInvoiceMaterialsHandler(response http.ResponseWriter, request *http.Request) {
	tenant, ok := materialTenant(response, request)
	if !ok {
		return
	}
	if request.URL.RawQuery != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	result, err := s.invoiceMaterials.Workspace(request.Context(), tenant, request.PathValue("invoice_id"))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) listMaterialCandidatesHandler(response http.ResponseWriter, request *http.Request) {
	tenant, ok := materialTenant(response, request)
	if !ok {
		return
	}
	values, err := url.ParseQuery(request.URL.RawQuery)
	if err != nil {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	input := invoicematerials.CandidateQuery{Limit: 20}
	for name, value := range values {
		if len(value) != 1 {
			writeError(response, request, domain.ErrInvalidInput)
			return
		}
		switch name {
		case "q":
			input.Query = value[0]
		case "cursor":
			input.Cursor = value[0]
		case "limit":
			input.Limit, err = strconv.Atoi(value[0])
		default:
			err = domain.ErrInvalidInput
		}
		if err != nil {
			writeError(response, request, domain.ErrInvalidInput)
			return
		}
	}
	result, err := s.invoiceMaterials.Candidates(request.Context(), tenant, request.PathValue("invoice_id"), input)
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) addInvoiceMaterialHandler(response http.ResponseWriter, request *http.Request) {
	s.changeInvoiceMaterial(response, request, "add")
}
func (s *Server) removeInvoiceMaterialHandler(response http.ResponseWriter, request *http.Request) {
	s.changeInvoiceMaterial(response, request, "remove")
}
func (s *Server) changeInvoiceMaterial(response http.ResponseWriter, request *http.Request, action string) {
	tenant, ok := materialTenant(response, request)
	if !ok {
		return
	}
	if request.URL.RawQuery != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	input := domain.InvoiceMaterialRequest{InvoiceID: request.PathValue("invoice_id"), Action: action}
	fields := map[string]any{"expected_version": &input.ExpectedVersion, "reason": &input.Reason, "idempotency_key": &input.IdempotencyKey}
	if action == "add" {
		fields["document_id"] = &input.DocumentID
	} else {
		input.LinkID = request.PathValue("link_id")
	}
	if err := decodeScalarFields(response, request, fields); err != nil {
		writeError(response, request, err)
		return
	}
	result, err := s.invoiceMaterials.Change(request.Context(), tenant, input, requestIDFromRequest(request))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (s *Server) uploadInvoiceMaterialHandler(response http.ResponseWriter, request *http.Request) {
	tenant, ok := materialTenant(response, request)
	if !ok {
		return
	}
	if request.URL.RawQuery != "" {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	file, fields, err := readDocumentMultipart(response, request, "expected_version", "reason", "idempotency_key")
	if err != nil {
		writeError(response, request, err)
		return
	}
	version, err := strconv.Atoi(fields["expected_version"])
	if err != nil {
		writeError(response, request, domain.ErrInvalidInput)
		return
	}
	input := domain.InvoiceMaterialRequest{InvoiceID: request.PathValue("invoice_id"), Action: "upload", ExpectedVersion: version, Reason: fields["reason"], IdempotencyKey: fields["idempotency_key"]}
	result, err := s.invoiceMaterials.Upload(request.Context(), tenant, input, file, requestIDFromRequest(request))
	if err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}
