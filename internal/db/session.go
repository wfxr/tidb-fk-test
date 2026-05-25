package db

import (
	"context"
	"database/sql"
)

// Session is the minimal database handle needed to start scenario transactions.
type Session interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error)
}

// Execer is the narrow non-transactional execution surface used during seeding.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// RowScanner captures the only row operation scenarios need after QueryRowContext.
type RowScanner interface {
	Scan(dest ...any) error
}

// Queryer is the narrow read surface used by checker queries.
type Queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) RowScanner
}

// TxSession is the narrow transaction surface scenarios use during execution.
type TxSession interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) RowScanner
}

// Tx is the runner-facing transaction handle that owns lifecycle operations.
type Tx interface {
	TxSession
	Commit() error
	Rollback() error
}
