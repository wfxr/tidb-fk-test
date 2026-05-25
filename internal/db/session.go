package db

import (
	"context"
	"database/sql"
)

// Session is the minimal database handle needed to start scenario transactions.
type Session interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error)
}

// RowScanner captures the only row operation scenarios need after QueryRowContext.
type RowScanner interface {
	Scan(dest ...any) error
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
