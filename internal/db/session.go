package db

import (
	"context"
	"database/sql"
)

// Session is the minimal database handle needed to start scenario transactions.
type Session interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error)
}

// TxSession is the narrow transaction surface scenarios use during execution.
type TxSession interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Tx is the runner-facing transaction handle that owns lifecycle operations.
type Tx interface {
	TxSession
	Commit() error
	Rollback() error
}
