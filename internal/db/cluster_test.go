package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenClusterDistributesTransactionsAcrossAliveNodes(t *testing.T) {
	state := newFakeClusterState()
	state.setAlive("node-a", true)
	state.setAlive("node-b", true)

	cluster := openTestCluster(t, state, []string{"node-a", "node-b"})
	defer closeCluster(t, cluster)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for range 100 {
		tx, err := cluster.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx() error = %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}
	}

	if got := state.beginCalls("node-a"); got == 0 {
		t.Fatal("node-a was never selected for BeginTx")
	}
	if got := state.beginCalls("node-b"); got == 0 {
		t.Fatal("node-b was never selected for BeginTx")
	}
}

func TestClusterBeginTxKeepsTransactionStickyAndInitializesSession(t *testing.T) {
	state := newFakeClusterState()
	state.setAlive("node-a", true)

	cluster := openTestCluster(t, state, []string{"node-a"})
	defer closeCluster(t, cluster)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	tx, err := cluster.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}

	if _, err := tx.ExecContext(ctx, "UPDATE sticky_test SET value = 1"); err != nil {
		t.Fatalf("ExecContext() error = %v", err)
	}

	var got string
	if err := tx.QueryRowContext(ctx, "SELECT current_dsn").Scan(&got); err != nil {
		t.Fatalf("QueryRowContext().Scan() error = %v", err)
	}
	if got != "node-a" {
		t.Fatalf("QueryRowContext().Scan() dsn = %q, want node-a", got)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	want := []string{
		enableSharedLockFKCheckSQL,
		"UPDATE sticky_test SET value = 1",
		"SELECT current_dsn",
		"COMMIT",
	}
	if got := state.txQueries("node-a"); !equalStrings(got, want) {
		t.Fatalf("node-a tx queries = %v, want %v", got, want)
	}
}

func TestClusterUsesAliveNodeForNonTransactionalQueriesAndBegins(t *testing.T) {
	state := newFakeClusterState()
	state.setAlive("node-a", false)
	state.setAlive("node-b", true)

	cluster := openTestCluster(t, state, []string{"node-a", "node-b"})
	defer closeCluster(t, cluster)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	var got string
	if err := cluster.QueryRowContext(ctx, "SELECT current_dsn").Scan(&got); err != nil {
		t.Fatalf("QueryRowContext().Scan() error = %v", err)
	}
	if got != "node-b" {
		t.Fatalf("QueryRowContext().Scan() dsn = %q, want node-b", got)
	}

	tx, err := cluster.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}

	if got := state.beginCalls("node-a"); got != 0 {
		t.Fatalf("node-a begin calls = %d, want 0", got)
	}
	if got := state.beginCalls("node-b"); got == 0 {
		t.Fatal("node-b was never selected for BeginTx")
	}
}

func TestClusterRefreshesLivenessAfterNodeDies(t *testing.T) {
	state := newFakeClusterState()
	state.setAlive("node-a", true)
	state.setAlive("node-b", true)

	cluster := openTestCluster(t, state, []string{"node-a", "node-b"})
	defer closeCluster(t, cluster)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	state.setAlive("node-a", false)
	time.Sleep(40 * time.Millisecond)

	for range 20 {
		tx, err := cluster.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx() after node death error = %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback() error = %v", err)
		}
	}

	if got := state.beginCalls("node-b"); got == 0 {
		t.Fatal("node-b was never selected after node-a died")
	}
	if got := state.beginFailures("node-a"); got != 0 {
		t.Fatalf("node-a begin failures after refresh = %d, want 0", got)
	}
}

func openTestCluster(t *testing.T, state *fakeClusterState, dsns []string) *Cluster {
	t.Helper()

	driverName := registerFakeClusterDriver(t, state)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	cluster, err := openCluster(ctx, clusterConfig{
		DriverName:     driverName,
		DSNs:           dsns,
		UpdateInterval: 10 * time.Millisecond,
		UpdateTimeout:  10 * time.Millisecond,
		SessionInitSQL: enableSharedLockFKCheckSQL,
	})
	if err != nil {
		t.Fatalf("openCluster() error = %v", err)
	}
	return cluster
}

func closeCluster(t *testing.T, cluster *Cluster) {
	t.Helper()
	if err := cluster.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

var fakeClusterDriverID atomic.Int64

func registerFakeClusterDriver(t *testing.T, state *fakeClusterState) string {
	t.Helper()

	name := fmt.Sprintf("fake-cluster-%d", fakeClusterDriverID.Add(1))
	sql.Register(name, fakeClusterDriver{state: state})
	return name
}

type fakeClusterDriver struct {
	state *fakeClusterState
}

func (d fakeClusterDriver) Open(name string) (driver.Conn, error) {
	return &fakeClusterConn{
		state: d.state,
		dsn:   name,
	}, nil
}

type fakeClusterConn struct {
	state       *fakeClusterState
	dsn         string
	currentTxID int64
}

func (c *fakeClusterConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("Prepare not implemented")
}

func (c *fakeClusterConn) Close() error {
	return nil
}

func (c *fakeClusterConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *fakeClusterConn) Ping(context.Context) error {
	if !c.state.isAlive(c.dsn) {
		return fmt.Errorf("node %s is down", c.dsn)
	}
	return nil
}

func (c *fakeClusterConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if !c.state.isAlive(c.dsn) {
		c.state.recordBeginFailure(c.dsn)
		return nil, fmt.Errorf("node %s is down", c.dsn)
	}

	txID := c.state.recordBegin(c.dsn)
	c.currentTxID = txID
	return &fakeClusterTx{
		state: c.state,
		conn:  c,
		dsn:   c.dsn,
		txID:  txID,
	}, nil
}

func (c *fakeClusterConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if !c.state.isAlive(c.dsn) {
		return nil, fmt.Errorf("node %s is down", c.dsn)
	}
	if c.currentTxID != 0 {
		c.state.recordTxQuery(c.dsn, c.currentTxID, query)
		return driver.RowsAffected(1), nil
	}
	c.state.recordExec(c.dsn, query)
	return driver.RowsAffected(1), nil
}

func (c *fakeClusterConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if !c.state.isAlive(c.dsn) {
		return nil, fmt.Errorf("node %s is down", c.dsn)
	}
	if c.currentTxID != 0 {
		c.state.recordTxQuery(c.dsn, c.currentTxID, query)
		return rowsForQuery(c.dsn, query), nil
	}
	c.state.recordQuery(c.dsn, query)
	return rowsForQuery(c.dsn, query), nil
}

type fakeClusterTx struct {
	state *fakeClusterState
	conn  *fakeClusterConn
	dsn   string
	txID  int64
}

func (tx *fakeClusterTx) Commit() error {
	tx.conn.currentTxID = 0
	tx.state.recordTxQuery(tx.dsn, tx.txID, "COMMIT")
	return nil
}

func (tx *fakeClusterTx) Rollback() error {
	tx.conn.currentTxID = 0
	tx.state.recordTxQuery(tx.dsn, tx.txID, "ROLLBACK")
	return nil
}

func (tx *fakeClusterTx) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if !tx.state.isAlive(tx.dsn) {
		return nil, fmt.Errorf("node %s is down", tx.dsn)
	}
	tx.state.recordTxQuery(tx.dsn, tx.txID, query)
	return driver.RowsAffected(1), nil
}

func (tx *fakeClusterTx) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if !tx.state.isAlive(tx.dsn) {
		return nil, fmt.Errorf("node %s is down", tx.dsn)
	}
	tx.state.recordTxQuery(tx.dsn, tx.txID, query)
	return rowsForQuery(tx.dsn, query), nil
}

type fakeClusterRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func rowsForQuery(dsn, query string) *fakeClusterRows {
	if query == "SELECT 1" {
		return &fakeClusterRows{
			columns: []string{"value"},
			values:  []driver.Value{int64(1)},
		}
	}
	return &fakeClusterRows{
		columns: []string{"current_dsn"},
		values:  []driver.Value{dsn},
	}
}

func (r *fakeClusterRows) Columns() []string {
	return r.columns
}

func (r *fakeClusterRows) Close() error {
	return nil
}

func (r *fakeClusterRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	for i := range r.values {
		dest[i] = r.values[i]
	}
	r.read = true
	return nil
}

type fakeClusterState struct {
	mu            sync.Mutex
	alive         map[string]bool
	beginCount    map[string]int
	beginFail     map[string]int
	execs         map[string][]string
	queries       map[string][]string
	txQueriesByID map[string]map[int64][]string
	nextTxID      int64
}

func newFakeClusterState() *fakeClusterState {
	return &fakeClusterState{
		alive:         make(map[string]bool),
		beginCount:    make(map[string]int),
		beginFail:     make(map[string]int),
		execs:         make(map[string][]string),
		queries:       make(map[string][]string),
		txQueriesByID: make(map[string]map[int64][]string),
	}
}

func (s *fakeClusterState) setAlive(dsn string, alive bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alive[dsn] = alive
}

func (s *fakeClusterState) isAlive(dsn string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alive[dsn]
}

func (s *fakeClusterState) recordBegin(dsn string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.beginCount[dsn]++
	s.nextTxID++
	if s.txQueriesByID[dsn] == nil {
		s.txQueriesByID[dsn] = make(map[int64][]string)
	}
	return s.nextTxID
}

func (s *fakeClusterState) recordBeginFailure(dsn string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.beginFail[dsn]++
}

func (s *fakeClusterState) recordExec(dsn, query string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execs[dsn] = append(s.execs[dsn], query)
}

func (s *fakeClusterState) recordQuery(dsn, query string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries[dsn] = append(s.queries[dsn], query)
}

func (s *fakeClusterState) recordTxQuery(dsn string, txID int64, query string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.txQueriesByID[dsn] == nil {
		s.txQueriesByID[dsn] = make(map[int64][]string)
	}
	s.txQueriesByID[dsn][txID] = append(s.txQueriesByID[dsn][txID], query)
}

func (s *fakeClusterState) beginCalls(dsn string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beginCount[dsn]
}

func (s *fakeClusterState) beginFailures(dsn string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beginFail[dsn]
}

func (s *fakeClusterState) txQueries(dsn string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	txMap := s.txQueriesByID[dsn]
	if len(txMap) != 1 {
		return nil
	}
	for _, queries := range txMap {
		return append([]string(nil), queries...)
	}
	return nil
}
