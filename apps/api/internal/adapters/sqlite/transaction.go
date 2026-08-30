package sqliteadapter

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type transaction struct {
	tx *sql.Tx
}

func (s *Store) WithinTransaction(ctx context.Context, operation func(ports.Transaction) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()
	if err := operation(transaction{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
