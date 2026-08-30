package httpapi

import (
	"context"
	"net/http"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type contextKey string

const (
	principalKey contextKey = "principal"
	requestIDKey contextKey = "request_id"
)

func withPrincipal(ctx context.Context, principal ports.SessionPrincipal) context.Context {
	return context.WithValue(ctx, principalKey, principal)
}

func principalFromRequest(request *http.Request) (ports.SessionPrincipal, bool) {
	principal, ok := request.Context().Value(principalKey).(ports.SessionPrincipal)
	return principal, ok
}

func requestIDFromRequest(request *http.Request) string {
	requestID, _ := request.Context().Value(requestIDKey).(string)
	return requestID
}
