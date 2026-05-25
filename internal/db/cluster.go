package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	hasql "golang.yandex/hasql/v2"
)

const enableSharedLockFKCheckSQL = "SET SESSION tidb_foreign_key_check_in_shared_lock = 1"

const (
	defaultClusterUpdateInterval = time.Second
	defaultClusterUpdateTimeout  = time.Second
	defaultClusterStartupWait    = 5 * time.Second
)

type clusterConfig struct {
	DriverName     string
	DSNs           []string
	UpdateInterval time.Duration
	UpdateTimeout  time.Duration
	StartupWait    time.Duration
	SessionInitSQL string
}

// Cluster owns the TiDB node pools and routes app traffic to alive nodes.
type Cluster struct {
	cluster        *hasql.Cluster[*sql.DB]
	sessionInitSQL string
}

func OpenTiDBCluster(ctx context.Context, dsns []string) (*Cluster, error) {
	return openCluster(ctx, clusterConfig{
		DriverName:     "mysql",
		DSNs:           dsns,
		UpdateInterval: defaultClusterUpdateInterval,
		UpdateTimeout:  defaultClusterUpdateTimeout,
		StartupWait:    defaultClusterStartupWait,
		SessionInitSQL: enableSharedLockFKCheckSQL,
	})
}

func openCluster(ctx context.Context, cfg clusterConfig) (*Cluster, error) {
	dsns := normalizeDSNs(cfg.DSNs)
	if len(dsns) == 0 {
		return nil, errors.New("at least one dsn is required")
	}
	if strings.TrimSpace(cfg.DriverName) == "" {
		return nil, errors.New("sql driver name is required")
	}
	if !sqlDriverRegistered(cfg.DriverName) {
		return nil, fmt.Errorf("sql driver %q is not registered", cfg.DriverName)
	}

	nodes := make([]*hasql.Node[*sql.DB], 0, len(dsns))
	openedDBs := make([]*sql.DB, 0, len(dsns))
	for i, dsn := range dsns {
		db, err := sql.Open(cfg.DriverName, dsn)
		if err != nil {
			closeAllDBs(openedDBs)
			return nil, err
		}
		openedDBs = append(openedDBs, db)
		nodes = append(nodes, hasql.NewNode(fmt.Sprintf("tidb-%d", i+1), db))
	}

	if err := preflightCluster(ctx, openedDBs, cfg.StartupWait); err != nil {
		closeAllDBs(openedDBs)
		return nil, err
	}

	cl, err := hasql.NewCluster(
		hasql.NewStaticNodeDiscoverer(nodes...),
		tidbChecker,
		hasql.WithUpdateInterval[*sql.DB](defaultDuration(cfg.UpdateInterval, defaultClusterUpdateInterval)),
		hasql.WithUpdateTimeout[*sql.DB](defaultDuration(cfg.UpdateTimeout, defaultClusterUpdateTimeout)),
	)
	if err != nil {
		closeAllDBs(openedDBs)
		return nil, err
	}

	waitCtx := ctx
	cancel := func() {}
	if cfg.StartupWait > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, cfg.StartupWait)
	}
	defer cancel()

	if _, err := cl.WaitForNode(waitCtx, hasql.Alive); err != nil {
		err = errors.Join(err, cl.Close(), closeAllDBs(openedDBs))
		return nil, err
	}

	return &Cluster{
		cluster:        cl,
		sessionInitSQL: cfg.SessionInitSQL,
	}, nil
}

func (c *Cluster) Close() error {
	if c == nil || c.cluster == nil {
		return nil
	}
	return c.cluster.Close()
}

func (c *Cluster) BeginTx(ctx context.Context, opts *sql.TxOptions) (Tx, error) {
	nodes, err := c.candidateNodes(ctx)
	if err != nil {
		return nil, err
	}

	var beginErr error
	for _, node := range nodes {
		tx, err := node.DB().BeginTx(ctx, opts)
		if err != nil {
			beginErr = errors.Join(beginErr, err)
			continue
		}
		if initSQL := c.sessionInitSQL; initSQL != "" {
			if _, err := tx.ExecContext(ctx, initSQL); err != nil {
				_ = tx.Rollback()
				beginErr = errors.Join(beginErr, err)
				continue
			}
		}
		return sqlTx{Tx: tx}, nil
	}

	if beginErr != nil {
		return nil, beginErr
	}
	return nil, errors.New("no alive tidb nodes available")
}

func (c *Cluster) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	nodes, err := c.candidateNodes(ctx)
	if err != nil {
		return nil, err
	}

	var execErr error
	for _, node := range nodes {
		result, err := node.DB().ExecContext(ctx, query, args...)
		if err == nil {
			return result, nil
		}
		execErr = errors.Join(execErr, err)
	}
	if execErr != nil {
		return nil, execErr
	}
	return nil, errors.New("no alive tidb nodes available")
}

func (c *Cluster) QueryRowContext(ctx context.Context, query string, args ...any) RowScanner {
	nodes, err := c.candidateNodes(ctx)
	if err != nil {
		return errorRow{err: err}
	}

	dbs := make([]*sql.DB, 0, len(nodes))
	for _, node := range nodes {
		dbs = append(dbs, node.DB())
	}

	return clusterRow{
		ctx:   ctx,
		dbs:   dbs,
		query: query,
		args:  append([]any(nil), args...),
	}
}

func (c *Cluster) candidateNodes(ctx context.Context) ([]*hasql.Node[*sql.DB], error) {
	if c == nil || c.cluster == nil {
		return nil, errors.New("cluster is not initialized")
	}

	first := c.cluster.Node(hasql.Alive)
	if first == nil {
		node, err := c.cluster.WaitForNode(ctx, hasql.Alive)
		if err != nil {
			if clusterErr := c.cluster.Err(); clusterErr != nil {
				return nil, errors.Join(clusterErr, err)
			}
			return nil, err
		}
		first = node
	}

	nodes := []*hasql.Node[*sql.DB]{first}
	for node := range c.cluster.NodesIter(hasql.Alive) {
		if node == first {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func normalizeDSNs(dsns []string) []string {
	normalized := make([]string, 0, len(dsns))
	for _, dsn := range dsns {
		if trimmed := strings.TrimSpace(dsn); trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	return normalized
}

func sqlDriverRegistered(name string) bool {
	for _, driverName := range sql.Drivers() {
		if driverName == name {
			return true
		}
	}
	return false
}

func closeAllDBs(dbs []*sql.DB) error {
	var err error
	for _, db := range dbs {
		err = errors.Join(err, db.Close())
	}
	return err
}

func defaultDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func preflightCluster(ctx context.Context, dbs []*sql.DB, startupWait time.Duration) error {
	if len(dbs) == 0 {
		return errors.New("at least one dsn is required")
	}

	timeout := time.Second
	if startupWait > 0 && startupWait < timeout {
		timeout = startupWait
	}

	var errs []error
	for _, db := range dbs {
		checkCtx := ctx
		cancel := func() {}
		if timeout > 0 {
			checkCtx, cancel = context.WithTimeout(ctx, timeout)
		}

		var reachable int
		err := db.QueryRowContext(checkCtx, "SELECT 1").Scan(&reachable)
		cancel()
		if err == nil {
			return nil
		}
		errs = append(errs, err)
	}

	return fmt.Errorf("preflight failed: %w", errors.Join(errs...))
}

func tidbChecker(ctx context.Context, db hasql.Querier) (hasql.NodeInfoProvider, error) {
	start := time.Now()

	var reachable int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&reachable); err != nil {
		return nil, err
	}

	return hasql.NodeInfo{
		ClusterRole:    hasql.NodeRolePrimary,
		NetworkLatency: time.Since(start),
		ReplicaLag:     0,
	}, nil
}

type clusterRow struct {
	ctx   context.Context
	dbs   []*sql.DB
	query string
	args  []any
}

func (r clusterRow) Scan(dest ...any) error {
	var err error
	for _, db := range r.dbs {
		scanErr := db.QueryRowContext(r.ctx, r.query, r.args...).Scan(dest...)
		if scanErr == nil {
			return nil
		}
		err = errors.Join(err, scanErr)
	}
	if err != nil {
		return err
	}
	return sql.ErrNoRows
}

type errorRow struct {
	err error
}

func (r errorRow) Scan(_ ...any) error {
	return r.err
}

type sqlTx struct {
	Tx *sql.Tx
}

func (tx sqlTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, query, args...)
}

func (tx sqlTx) QueryRowContext(ctx context.Context, query string, args ...any) RowScanner {
	return tx.Tx.QueryRowContext(ctx, query, args...)
}

func (tx sqlTx) Commit() error {
	return tx.Tx.Commit()
}

func (tx sqlTx) Rollback() error {
	return tx.Tx.Rollback()
}
