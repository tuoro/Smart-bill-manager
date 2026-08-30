package sqliteadapter

import (
	"errors"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
)

func TestAllocationInsertErrorMapsPairUniqueConstraint(t *testing.T) {
	err := allocationInsertError(errors.New(
		"UNIQUE constraint failed: payment_invoice_links.tenant_id, payment_invoice_links.payment_id, payment_invoice_links.invoice_id",
	))
	var rule *domain.RuleError
	if !errors.As(err, &rule) || rule.Code != "allocation_pair_conflict" || !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("pair conflict mapping = %v", err)
	}
}
