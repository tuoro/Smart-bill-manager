package postgresqladapter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type transaction struct {
	tx *sql.Tx
}

func (s *Store) WithinTransaction(ctx context.Context, operation func(ports.Transaction) error) error {
	const maxAttempts = 8
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := s.runTransactionAttempt(ctx, sql.LevelSerializable, operation)
		if err == nil {
			return nil
		}
		if !isRetryableTransactionError(err) || attempt == maxAttempts {
			return normalizeTransactionError(err)
		}
		if err := waitForTransactionRetry(ctx, attempt); err != nil {
			return err
		}
	}
	return fmt.Errorf("postgresql transaction retry exhausted: %w", domain.ErrConflict)
}

func (s *Store) WithinReadCommittedTransaction(ctx context.Context, operation func(ports.Transaction) error) error {
	return normalizeTransactionError(s.runTransactionAttempt(ctx, sql.LevelReadCommitted, operation))
}

func waitForTransactionRetry(ctx context.Context, attempt int) error {
	delay := 5 * time.Millisecond * time.Duration(1<<min(attempt-1, 4))
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Store) runTransactionAttempt(ctx context.Context, isolation sql.IsolationLevel, operation func(ports.Transaction) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: isolation})
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

func normalizeTransactionError(err error) error {
	if isRetryableTransactionError(err) {
		return fmt.Errorf("postgresql transaction conflict: %w", domain.ErrConflict)
	}
	return err
}

func isRetryableTransactionError(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && (postgresError.Code == "40001" || postgresError.Code == "40P01")
}
