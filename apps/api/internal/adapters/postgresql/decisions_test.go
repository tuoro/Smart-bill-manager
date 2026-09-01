package postgresqladapter

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestAllocationInsertErrorMapsPairUniqueConstraint(t *testing.T) {
	err := allocationInsertError(&pgconn.PgError{
		Code: "23505", ConstraintName: "payment_invoice_links_pair_active_idx",
	})
	var rule *domain.RuleError
	if !errors.As(err, &rule) || rule.Code != "allocation_pair_conflict" || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("pair conflict mapping = %v", err)
	}
}
