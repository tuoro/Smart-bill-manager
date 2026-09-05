package ports

import (
	"context"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"time"
)

type BadDebtResult struct {
	DecisionID string `json:"decision_id"`
	Version    int    `json:"version"`
	Marked     bool   `json:"marked"`
	Replayed   bool   `json:"replayed"`
}

type BadDebtCommand struct {
	TenantID, ActorUserID, FactID, DecisionID, AuditEventID, RequestID, IdempotencyKey, RequestHash string
	FactType                                                                                        domain.DocumentType
	Input                                                                                           domain.BadDebtInput
	CreatedAt                                                                                       time.Time
}

type BadDebtTransaction interface {
	SetFactBadDebt(context.Context, BadDebtCommand) (BadDebtResult, error)
}
