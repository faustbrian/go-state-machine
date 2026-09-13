package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cleanupTimeout = 5 * time.Second

type operationError struct {
	operation string
	cause     error
}

func (err *operationError) Error() string {
	return fmt.Sprintf("postgres: %s failed", err.operation)
}

func (err *operationError) Unwrap() error {
	return err.cause
}

func wrapError(operation string, cause error) error {
	return &operationError{operation: operation, cause: cause}
}

func rollback(tx transaction) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	_ = tx.Rollback(ctx)
}

type commandResult interface {
	RowsAffected() int64
}

type row interface {
	Scan(...any) error
}

type rows interface {
	Close()
	Err() error
	Next() bool
	Scan(...any) error
}

type transaction interface {
	Exec(context.Context, string, ...any) (commandResult, error)
	Query(context.Context, string, ...any) (rows, error)
	QueryRow(context.Context, string, ...any) row
	Commit(context.Context) error
	Rollback(context.Context) error
}

type database interface {
	Exec(context.Context, string, ...any) (commandResult, error)
	Query(context.Context, string, ...any) (rows, error)
	QueryRow(context.Context, string, ...any) row
	Begin(context.Context) (transaction, error)
}

type poolDatabase struct {
	pool *pgxpool.Pool
}

func (database poolDatabase) Exec(ctx context.Context, sql string, arguments ...any) (commandResult, error) {
	return database.pool.Exec(ctx, sql, arguments...)
}

func (database poolDatabase) Query(ctx context.Context, sql string, arguments ...any) (rows, error) {
	return database.pool.Query(ctx, sql, arguments...)
}

func (database poolDatabase) QueryRow(ctx context.Context, sql string, arguments ...any) row {
	return database.pool.QueryRow(ctx, sql, arguments...)
}

func (database poolDatabase) Begin(ctx context.Context) (transaction, error) {
	tx, err := database.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return transactionAdapter{tx: tx}, nil
}

type transactionAdapter struct {
	tx pgx.Tx
}

func (adapter transactionAdapter) Exec(ctx context.Context, sql string, arguments ...any) (commandResult, error) {
	return adapter.tx.Exec(ctx, sql, arguments...)
}

func (adapter transactionAdapter) Query(ctx context.Context, sql string, arguments ...any) (rows, error) {
	return adapter.tx.Query(ctx, sql, arguments...)
}

func (adapter transactionAdapter) QueryRow(ctx context.Context, sql string, arguments ...any) row {
	return adapter.tx.QueryRow(ctx, sql, arguments...)
}

func (adapter transactionAdapter) Commit(ctx context.Context) error {
	return adapter.tx.Commit(ctx)
}

func (adapter transactionAdapter) Rollback(ctx context.Context) error {
	return adapter.tx.Rollback(ctx)
}
