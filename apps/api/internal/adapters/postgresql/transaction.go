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
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		if pgError.Code == "23505" && pgError.ConstraintName == "fact_bad_debt_idempotency_unique" {
			return domain.NewRuleError("idempotency_key_conflict", "幂等键已用于不同的坏账操作", domain.ErrConflict)
		}
		switch pgError.Message {
		case "trip_bad_debt_locked":
			return domain.NewRuleError("trip_bad_debt_locked", "行程关联坏账单据，请先处理坏账或调整关联后再删除", domain.ErrConflict)
		case "allocation_active_target_limit_exceeded":
			return domain.NewRuleError("allocation_active_target_limit_exceeded", "单据最多保留 200 条活动分配，请先调整已有分配", domain.ErrConflict)
		}
	}
	if isRetryableTransactionError(err) {
		return fmt.Errorf("postgresql transaction conflict: %w", domain.ErrConflict)
	}
	return err
}

func isRetryableTransactionError(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && (postgresError.Code == "40001" || postgresError.Code == "40P01")
}
