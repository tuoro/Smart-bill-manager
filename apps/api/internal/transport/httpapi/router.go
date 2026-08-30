package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/allocations"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/auth"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/documents"
	applicationemails "github.com/tuoro/smart-bill-manager/apps/api/internal/application/emails"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/providers"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reimbursements"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/reviews"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/trips"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

const (
	sessionCookieName = "sbm_session"
	csrfCookieName    = "sbm_csrf"
)

type HealthChecker interface {
	Ping(ctx context.Context) error
}

type ReadinessChecker interface {
	Ready(ctx context.Context) error
}

type Config struct {
	CookieSecure bool
	Version      string
	WebDistPath  string
}

type Server struct {
	auth           auth.Service
	health         HealthChecker
	readiness      ReadinessChecker
	ids            system.IDGenerator
	logger         *slog.Logger
	config         Config
	upload         documents.UploadService
	documents      documents.QueryService
	jobActions     documents.ActionService
	deletions      documents.DeletionService
	providers      providers.Service
	reviews        reviews.Service
	facts          reviews.FactService
	allocations    allocations.Service
	emails         applicationemails.Service
	trips          trips.Service
	reimbursements reimbursements.Service
	spa            http.Handler
}

func NewServer(
	authService auth.Service,
	uploadService documents.UploadService,
	documentQueries documents.QueryService,
	jobActions documents.ActionService,
	documentDeletions documents.DeletionService,
	providerService providers.Service,
	reviewService reviews.Service,
	factService reviews.FactService,
	allocationService allocations.Service,
	emailService applicationemails.Service,
	tripService trips.Service,
	reimbursementService reimbursements.Service,
	health HealthChecker,
	readiness ReadinessChecker,
	logger *slog.Logger,
	config Config,
) (*Server, error) {
	spa, err := newSPAHandler(config.WebDistPath)
	if err != nil {
		return nil, fmt.Errorf("configure web application: %w", err)
	}
	return &Server{
		auth:           authService,
		upload:         uploadService,
		documents:      documentQueries,
		jobActions:     jobActions,
		deletions:      documentDeletions,
		providers:      providerService,
		reviews:        reviewService,
		facts:          factService,
		allocations:    allocationService,
		emails:         emailService,
		trips:          tripService,
		reimbursements: reimbursementService,
		health:         health,
		readiness:      readiness,
		ids:            system.IDGenerator{},
		logger:         logger,
		config:         config,
		spa:            spa,
	}, nil
}

func (s *Server) Handler() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /api/v1/health", s.healthHandler)
	router.HandleFunc("GET /api/v1/ready", s.readinessHandler)
	router.HandleFunc("POST /api/v1/session/login", s.loginHandler)
	router.Handle("GET /api/v1/session", s.requireSession(http.HandlerFunc(s.sessionHandler)))
	router.Handle("DELETE /api/v1/session", s.requireSession(s.requireCSRF(http.HandlerFunc(s.logoutHandler))))
	router.Handle("POST /api/v1/documents", s.requireSession(s.requireCSRF(http.HandlerFunc(s.uploadDocumentHandler))))
	router.Handle("POST /api/v1/email-sources", s.requireSession(s.requireCSRF(http.HandlerFunc(s.registerEmailSourceHandler))))
	router.Handle("GET /api/v1/email-sources", s.requireSession(http.HandlerFunc(s.listEmailSourcesHandler)))
	router.Handle("GET /api/v1/email-sources/{source_id}/messages", s.requireSession(http.HandlerFunc(s.listEmailMessagesHandler)))
	router.Handle("GET /api/v1/email-messages/{message_id}/raw", s.requireSession(http.HandlerFunc(s.downloadEmailMessageHandler)))
	router.Handle("GET /api/v1/email-attachments/{attachment_id}/content", s.requireSession(http.HandlerFunc(s.downloadEmailAttachmentHandler)))
	router.Handle("DELETE /api/v1/documents/{document_id}", s.requireSession(s.requireCSRF(http.HandlerFunc(s.deleteDocumentHandler))))
	router.Handle("GET /api/v1/documents/{document_id}/content", s.requireSession(http.HandlerFunc(s.downloadDocumentHandler)))
	router.Handle("GET /api/v1/documents/{document_id}/pages/{page_number}/content", s.requireSession(http.HandlerFunc(s.downloadDocumentPageHandler)))
	router.Handle("GET /api/v1/documents/{document_id}", s.requireSession(http.HandlerFunc(s.getDocumentHandler)))
	router.Handle("GET /api/v1/jobs", s.requireSession(http.HandlerFunc(s.listJobsHandler)))
	router.Handle("GET /api/v1/jobs/{job_id}", s.requireSession(http.HandlerFunc(s.getJobHandler)))
	router.Handle("POST /api/v1/jobs/{job_id}/cancel", s.requireSession(s.requireCSRF(http.HandlerFunc(s.cancelJobHandler))))
	router.Handle("POST /api/v1/jobs/{job_id}/retry", s.requireSession(s.requireCSRF(http.HandlerFunc(s.retryJobHandler))))
	router.Handle("GET /api/v1/reviews/{job_id}", s.requireSession(http.HandlerFunc(s.getReviewHandler)))
	router.Handle("GET /api/v1/claim-sets/{claim_set_id}", s.requireSession(http.HandlerFunc(s.getClaimSetHandler)))
	router.Handle("POST /api/v1/reviews/{job_id}/revisions", s.requireSession(s.requireCSRF(http.HandlerFunc(s.reviseReviewHandler))))
	router.Handle("POST /api/v1/reviews/{job_id}/confirm", s.requireSession(s.requireCSRF(http.HandlerFunc(s.confirmReviewHandler))))
	router.Handle("POST /api/v1/reviews/{job_id}/reject", s.requireSession(s.requireCSRF(http.HandlerFunc(s.rejectReviewHandler))))
	router.Handle("GET /api/v1/payments", s.requireSession(http.HandlerFunc(s.listPaymentsHandler)))
	router.Handle("DELETE /api/v1/payments/{payment_id}", s.requireSession(s.requireCSRF(http.HandlerFunc(s.deletePaymentHandler))))
	router.Handle("GET /api/v1/invoices", s.requireSession(http.HandlerFunc(s.listInvoicesHandler)))
	router.Handle("DELETE /api/v1/invoices/{invoice_id}", s.requireSession(s.requireCSRF(http.HandlerFunc(s.deleteInvoiceHandler))))
	router.Handle("GET /api/v1/trips", s.requireSession(http.HandlerFunc(s.listTripsHandler)))
	router.Handle("DELETE /api/v1/trips/{trip_id}", s.requireSession(s.requireCSRF(http.HandlerFunc(s.deleteTripHandler))))
	router.Handle("GET /api/v1/trips/{trip_id}/attribution-candidates", s.requireSession(http.HandlerFunc(s.listTripAttributionCandidatesHandler)))
	router.Handle("POST /api/v1/trip-assignments", s.requireSession(s.requireCSRF(http.HandlerFunc(s.assignTripFactHandler))))
	router.Handle("POST /api/v1/reimbursement-previews", s.requireSession(s.requireCSRF(http.HandlerFunc(s.previewReimbursementHandler))))
	router.Handle("POST /api/v1/reimbursements", s.requireSession(s.requireCSRF(http.HandlerFunc(s.submitReimbursementHandler))))
	router.Handle("GET /api/v1/reimbursements", s.requireSession(http.HandlerFunc(s.listReimbursementsHandler)))
	router.Handle("GET /api/v1/reimbursements/{reimbursement_id}", s.requireSession(http.HandlerFunc(s.getReimbursementHandler)))
	router.Handle("POST /api/v1/reimbursements/{reimbursement_id}/status-decisions", s.requireSession(s.requireCSRF(http.HandlerFunc(s.changeReimbursementStatusHandler))))
	router.Handle("GET /api/v1/allocations/{fact_type}/{fact_id}", s.requireSession(http.HandlerFunc(s.getAllocationWorkspaceHandler)))
	router.Handle("POST /api/v1/allocations/{fact_type}/{fact_id}/adjustments", s.requireSession(s.requireCSRF(http.HandlerFunc(s.adjustAllocationHandler))))
	router.Handle("GET /api/v1/provider-configs", s.requireSession(http.HandlerFunc(s.listProviderConfigsHandler)))
	router.Handle("POST /api/v1/provider-configs", s.requireSession(s.requireCSRF(http.HandlerFunc(s.createProviderConfigHandler))))
	router.Handle("POST /api/v1/provider-configs/{provider_config_id}/detect", s.requireSession(s.requireCSRF(http.HandlerFunc(s.detectProviderConfigHandler))))
	router.Handle("POST /api/v1/provider-configs/{provider_config_id}/activate", s.requireSession(s.requireCSRF(http.HandlerFunc(s.activateProviderConfigHandler))))
	router.Handle("DELETE /api/v1/provider-configs/{provider_config_id}", s.requireSession(s.requireCSRF(http.HandlerFunc(s.deleteProviderConfigHandler))))
	router.Handle("GET /", s.spa)
	return s.securityHeaders(s.requestContext(s.recoverPanic(router)))
}

func (s *Server) healthHandler(response http.ResponseWriter, request *http.Request) {
	if err := s.health.Ping(request.Context()); err != nil {
		writeError(response, request, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok", "version": s.config.Version})
}

func (s *Server) readinessHandler(response http.ResponseWriter, request *http.Request) {
	if err := s.readiness.Ready(request.Context()); err != nil {
		writeError(response, request, domain.NewRuleError("not_ready", "服务尚未就绪", domain.ErrUnavailable))
		return
	}
	writeJSON(response, http.StatusOK, map[string]string{"status": "ready", "version": s.config.Version})
}

func (s *Server) requestContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestID, err := s.ids.NewID()
		if err != nil {
			http.Error(response, "request id unavailable", http.StatusInternalServerError)
			return
		}
		response.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(request.Context(), requestIDKey, requestID)
		next.ServeHTTP(response, request.WithContext(ctx))
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("http panic", "request_id", requestIDFromRequest(request), "stack", string(debug.Stack()))
				writeError(response, request, domain.NewRuleError("internal_error", "服务暂时无法完成请求", nil))
			}
		}()
		next.ServeHTTP(response, request)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		response.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		response.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		if s.config.CookieSecure {
			response.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if strings.HasPrefix(request.URL.Path, "/api/") {
			response.Header().Set("Cache-Control", "no-store")
			response.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		} else {
			response.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; frame-src 'self'; img-src 'self' data: blob:; object-src 'none'; script-src 'self'; style-src 'self'")
		}
		next.ServeHTTP(response, request)
	})
}

func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		cookie, err := request.Cookie(sessionCookieName)
		if err != nil {
			writeError(response, request, domain.ErrUnauthenticated)
			return
		}
		principal, err := s.auth.Authenticate(request.Context(), cookie.Value)
		if err != nil {
			writeError(response, request, err)
			return
		}
		next.ServeHTTP(response, request.WithContext(withPrincipal(request.Context(), principal)))
	})
}

func (s *Server) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		principal, ok := principalFromRequest(request)
		if !ok {
			writeError(response, request, domain.ErrUnauthenticated)
			return
		}
		if err := s.auth.VerifyCSRF(principal, request.Header.Get("X-CSRF-Token")); err != nil {
			writeError(response, request, err)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (s *Server) setSessionCookies(response http.ResponseWriter, view auth.SessionView) {
	maxAge := max(int(time.Until(view.ExpiresAt).Seconds()), 1)
	http.SetCookie(response, &http.Cookie{
		Name: sessionCookieName, Value: view.SessionToken, Path: "/",
		Expires: view.ExpiresAt, MaxAge: maxAge, Secure: s.config.CookieSecure,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(response, &http.Cookie{
		Name: csrfCookieName, Value: view.CSRFToken, Path: "/",
		Expires: view.ExpiresAt, MaxAge: maxAge, Secure: s.config.CookieSecure,
		HttpOnly: false, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearSessionCookies(response http.ResponseWriter) {
	for _, name := range []string{sessionCookieName, csrfCookieName} {
		http.SetCookie(response, &http.Cookie{
			Name: name, Value: "", Path: "/", MaxAge: -1,
			Secure: s.config.CookieSecure, HttpOnly: name == sessionCookieName,
			SameSite: http.SameSiteStrictMode,
		})
	}
}

func tenantContext(principal ports.SessionPrincipal) domain.TenantContext {
	return domain.TenantContext{TenantID: principal.TenantID, UserID: principal.UserID, Role: principal.Role}
}
