package runner

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/db"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/model"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/report"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/scenario"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/seed"
)

func TestEngineRunExecutesWorkersAndRecordsSummary(t *testing.T) {
	seedState := scenario.SeedState{
		Generic:    seed.GenericSeedPlan{ExistingParentID: 77},
		PropertyMe: seed.PropertyMeSeedPlan{ExistingBillID: 88},
	}
	startedCh := make(chan string, 2)
	releaseCh := make(chan struct{})
	stopCh := make(chan time.Time, 1)
	genericScenario := newStubScenario("generic_success", scenario.GenericGroup, nil, func() {
		startedCh <- "generic_success"
		<-releaseCh
	})
	probeScenario := newStubScenario("payment_bill_update_probe", scenario.FailureProbeGroup, errors.New("upgrading a shared lock to an exclusive lock is not supported"), func() {
		startedCh <- "payment_bill_update_probe"
		<-releaseCh
	})
	registry := newStubRegistry(
		genericScenario,
		probeScenario,
	)
	summary := report.NewSummary(registry.All())
	session := &stubSession{}
	progress := report.NewProgressReporter(time.Second)
	engine := NewEngine(EngineConfig{
		Session:        session,
		Registry:       registry,
		Scheduler:      Scheduler{GenericWorkers: 1, FailureProbeWorkers: 1},
		SeedState:      seedState,
		Summary:        summary,
		Progress:       progress,
		WarmupDuration: time.Minute,
		After: func(time.Duration) <-chan time.Time {
			return stopCh
		},
		NewTicker: func(time.Duration) ticker {
			return &stubTicker{ch: make(chan time.Time)}
		},
		Now: func() time.Time {
			return time.Date(2026, time.May, 25, 10, 0, 0, 0, time.UTC)
		},
	})

	done := make(chan error, 1)
	go func() {
		done <- engine.Run(context.Background())
	}()

	gotStarts := []string{<-startedCh, <-startedCh}
	close(releaseCh)
	stopCh <- time.Date(2026, time.May, 25, 10, 0, 5, 0, time.UTC)

	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !reflect.DeepEqual(gotStarts, []string{"generic_success", "payment_bill_update_probe"}) &&
		!reflect.DeepEqual(gotStarts, []string{"payment_bill_update_probe", "generic_success"}) {
		t.Fatalf("worker starts = %v, want both worker scenarios", gotStarts)
	}

	if len(session.txs) != 2 {
		t.Fatalf("transactions opened = %d, want 2", len(session.txs))
	}
	genericRuns := genericScenario.Runs()
	if len(genericRuns) != 1 {
		t.Fatalf("generic scenario runs = %d, want 1", len(genericRuns))
	}
	if genericRuns[0].tx.commits != 1 {
		t.Fatalf("generic tx commits = %d, want 1", genericRuns[0].tx.commits)
	}
	if genericRuns[0].tx.rollbacks != 0 {
		t.Fatalf("generic tx rollbacks = %d, want 0", genericRuns[0].tx.rollbacks)
	}
	if !reflect.DeepEqual(genericRuns[0].seed, seedState) {
		t.Fatalf("generic seed = %#v, want %#v", genericRuns[0].seed, seedState)
	}
	probeRuns := probeScenario.Runs()
	if len(probeRuns) != 1 {
		t.Fatalf("probe scenario runs = %d, want 1", len(probeRuns))
	}
	if probeRuns[0].tx.commits != 0 {
		t.Fatalf("probe tx commits = %d, want 0", probeRuns[0].tx.commits)
	}
	if probeRuns[0].tx.rollbacks != 1 {
		t.Fatalf("probe tx rollbacks = %d, want 1", probeRuns[0].tx.rollbacks)
	}
	if !reflect.DeepEqual(probeRuns[0].seed, seedState) {
		t.Fatalf("probe seed = %#v, want %#v", probeRuns[0].seed, seedState)
	}

	totals := summary.Totals()
	if totals.Executed != 2 {
		t.Fatalf("Totals().Executed = %d, want 2", totals.Executed)
	}
	if totals.Success != 1 {
		t.Fatalf("Totals().Success = %d, want 1", totals.Success)
	}
	if totals.ExpectedFailure != 1 {
		t.Fatalf("Totals().ExpectedFailure = %d, want 1", totals.ExpectedFailure)
	}
	if totals.UnexpectedFailure != 0 {
		t.Fatalf("Totals().UnexpectedFailure = %d, want 0", totals.UnexpectedFailure)
	}
}

func TestEngineRunEmitsRecurringProgressSnapshots(t *testing.T) {
	registry := newStubRegistry(
		newStubScenario("generic_success", scenario.GenericGroup, nil, nil),
	)
	summary := report.NewSummary(registry.All())
	session := &stubSession{}
	tickerCh := make(chan time.Time, 2)
	stopCh := make(chan time.Time, 1)
	var snapshots []report.Snapshot
	engine := NewEngine(EngineConfig{
		Session:        session,
		Registry:       registry,
		Scheduler:      Scheduler{GenericWorkers: 1},
		SeedState:      scenario.SeedState{},
		Summary:        summary,
		Progress:       report.NewProgressReporter(250 * time.Millisecond),
		WarmupDuration: time.Minute,
		After: func(time.Duration) <-chan time.Time {
			return stopCh
		},
		NewTicker: func(time.Duration) ticker {
			return &stubTicker{ch: tickerCh}
		},
		Now: func() time.Time {
			return time.Date(2026, time.May, 25, 11, 0, 0, 0, time.UTC)
		},
		OnProgress: func(snapshot report.Snapshot) {
			snapshots = append(snapshots, snapshot)
			if len(snapshots) == 2 {
				stopCh <- time.Date(2026, time.May, 25, 11, 0, 2, 0, time.UTC)
			}
		},
	})

	done := make(chan error, 1)
	go func() {
		done <- engine.Run(context.Background())
	}()

	tickerCh <- time.Date(2026, time.May, 25, 11, 0, 1, 0, time.UTC)
	tickerCh <- time.Date(2026, time.May, 25, 11, 0, 2, 0, time.UTC)

	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(snapshots) != 2 {
		t.Fatalf("progress snapshots = %d, want 2", len(snapshots))
	}
	for _, snapshot := range snapshots {
		if snapshot.Phase != WarmupPhase {
			t.Fatalf("snapshot.Phase = %q, want %q", snapshot.Phase, WarmupPhase)
		}
		if snapshot.ActiveWorkers != 1 {
			t.Fatalf("snapshot.ActiveWorkers = %d, want 1", snapshot.ActiveWorkers)
		}
	}
}

func TestEngineRunStopsAtWarmupDeadlineDeterministically(t *testing.T) {
	registry := newStubRegistry()
	summary := report.NewSummary(nil)
	session := &stubSession{}
	stopCh := make(chan time.Time, 1)
	engine := NewEngine(EngineConfig{
		Session:        session,
		Registry:       registry,
		Scheduler:      Scheduler{},
		SeedState:      scenario.SeedState{},
		Summary:        summary,
		Progress:       report.NewProgressReporter(time.Second),
		WarmupDuration: 3 * time.Second,
		After: func(got time.Duration) <-chan time.Time {
			if got != 3*time.Second {
				t.Fatalf("After() duration = %v, want 3s", got)
			}
			return stopCh
		},
		NewTicker: func(time.Duration) ticker {
			return &stubTicker{ch: make(chan time.Time)}
		},
		Now: func() time.Time {
			return time.Date(2026, time.May, 25, 12, 0, 0, 0, time.UTC)
		},
	})

	done := make(chan error, 1)
	go func() {
		done <- engine.Run(context.Background())
	}()

	stopCh <- time.Date(2026, time.May, 25, 12, 0, 3, 0, time.UTC)

	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if summary.Totals().Executed != 0 {
		t.Fatalf("Totals().Executed = %d, want 0", summary.Totals().Executed)
	}
}

func TestEngineRunIgnoresCanceledInFlightScenarioAfterStop(t *testing.T) {
	startedCh := make(chan struct{}, 1)
	stopCh := make(chan time.Time, 1)
	sc := newStubScenarioWithRunFunc("generic_success", scenario.GenericGroup, func(ctx context.Context) error {
		startedCh <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	registry := newStubRegistry(sc)
	summary := report.NewSummary(registry.All())
	session := &stubSession{}
	engine := NewEngine(EngineConfig{
		Session:        session,
		Registry:       registry,
		Scheduler:      Scheduler{GenericWorkers: 1},
		SeedState:      scenario.SeedState{},
		Summary:        summary,
		Progress:       report.NewProgressReporter(time.Second),
		WarmupDuration: time.Minute,
		After: func(time.Duration) <-chan time.Time {
			return stopCh
		},
		NewTicker: func(time.Duration) ticker {
			return &stubTicker{ch: make(chan time.Time)}
		},
		Now: func() time.Time {
			return time.Date(2026, time.May, 25, 12, 30, 0, 0, time.UTC)
		},
	})

	done := make(chan error, 1)
	go func() {
		done <- engine.Run(context.Background())
	}()

	<-startedCh
	stopCh <- time.Date(2026, time.May, 25, 12, 30, 1, 0, time.UTC)

	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if len(session.txs) != 1 {
		t.Fatalf("transactions opened = %d, want 1", len(session.txs))
	}
	if session.txs[0].rollbacks != 1 {
		t.Fatalf("tx rollbacks = %d, want 1", session.txs[0].rollbacks)
	}

	stats, ok := summary.Scenario("generic_success")
	if !ok {
		t.Fatal("Scenario(generic_success) not found")
	}
	if stats.Executed != 0 {
		t.Fatalf("Executed = %d, want 0", stats.Executed)
	}
	if stats.UnexpectedFailure != 0 {
		t.Fatalf("UnexpectedFailure = %d, want 0", stats.UnexpectedFailure)
	}
	if stats.LastErrorText != "" {
		t.Fatalf("LastErrorText = %q, want empty", stats.LastErrorText)
	}
	if got := summary.Totals(); got != (report.Totals{}) {
		t.Fatalf("Totals() = %#v, want zero totals", got)
	}
}

func TestEngineRunWorkerStepSkipsBeginTxAfterCancellation(t *testing.T) {
	registry := newStubRegistry(
		newStubScenario("generic_success", scenario.GenericGroup, nil, nil),
	)
	summary := report.NewSummary(registry.All())
	session := &stubSession{}
	engine := NewEngine(EngineConfig{
		Session:        session,
		Registry:       registry,
		Scheduler:      Scheduler{GenericWorkers: 1},
		SeedState:      scenario.SeedState{},
		Summary:        summary,
		Progress:       report.NewProgressReporter(time.Second),
		WarmupDuration: time.Minute,
		Now: func() time.Time {
			return time.Date(2026, time.May, 25, 12, 45, 0, 0, time.UTC)
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	engine.runWorkerStep(ctx, workerPlan{
		worker:   Worker{Group: scenario.GenericGroup},
		scenario: registry.all[0],
	})

	if session.beginCalls != 0 {
		t.Fatalf("BeginTx calls = %d, want 0", session.beginCalls)
	}
	if got := summary.Totals(); got != (report.Totals{}) {
		t.Fatalf("Totals() = %#v, want zero totals", got)
	}
}

type stubRegistry struct {
	byName map[string]scenario.Scenario
	all    []scenario.Scenario
}

func newStubRegistry(items ...scenario.Scenario) stubRegistry {
	byName := make(map[string]scenario.Scenario, len(items))
	for _, item := range items {
		byName[item.Meta().Name] = item
	}
	return stubRegistry{
		byName: byName,
		all:    append([]scenario.Scenario(nil), items...),
	}
}

func (r stubRegistry) All() []scenario.Scenario {
	return append([]scenario.Scenario(nil), r.all...)
}

func (r stubRegistry) Get(name string) (scenario.Scenario, bool) {
	item, ok := r.byName[name]
	return item, ok
}

type stubScenario struct {
	meta    scenario.Metadata
	err     error
	onRun   func()
	runFunc func(context.Context) error
	mu      sync.Mutex
	runs    []stubRun
}

type stubRun struct {
	seed scenario.SeedState
	tx   *stubTx
}

func newStubScenario(name string, group scenario.Group, err error, onRun func()) *stubScenario {
	return &stubScenario{
		meta: scenario.Metadata{
			Name:            name,
			Group:           group,
			Weight:          1,
			ConcurrencyHint: 1,
		},
		err:   err,
		onRun: onRun,
	}
}

func newStubScenarioWithRunFunc(name string, group scenario.Group, run func(context.Context) error) *stubScenario {
	return &stubScenario{
		meta: scenario.Metadata{
			Name:            name,
			Group:           group,
			Weight:          1,
			ConcurrencyHint: 1,
		},
		runFunc: run,
	}
}

func (s *stubScenario) Meta() scenario.Metadata {
	return s.meta
}

func (s *stubScenario) Run(ctx context.Context, sess dbpkg.TxSession, seed scenario.SeedState) error {
	tx, ok := sess.(*stubTx)
	if !ok {
		return errors.New("unexpected tx session type")
	}
	if s.onRun != nil {
		s.onRun()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs = append(s.runs, stubRun{seed: seed, tx: tx})
	if s.runFunc != nil {
		return s.runFunc(ctx)
	}
	return s.err
}

func (s *stubScenario) Runs() []stubRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]stubRun(nil), s.runs...)
}

type stubSession struct {
	mu         sync.Mutex
	beginCalls int
	txs        []*stubTx
	beginErr   error
	onBegin    func()
}

func (s *stubSession) BeginTx(context.Context, *sql.TxOptions) (dbpkg.Tx, error) {
	s.mu.Lock()
	s.beginCalls++
	onBegin := s.onBegin
	beginErr := s.beginErr
	s.mu.Unlock()

	if onBegin != nil {
		onBegin()
	}
	if beginErr != nil {
		return nil, beginErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx := &stubTx{}
	s.txs = append(s.txs, tx)
	return tx, nil
}

type stubTx struct {
	commits     int
	rollbacks   int
	commitErr   error
	rollbackErr error
	execErr     error
	queryRowErr error
}

func (tx *stubTx) Commit() error {
	tx.commits++
	return tx.commitErr
}

func (tx *stubTx) Rollback() error {
	tx.rollbacks++
	return tx.rollbackErr
}

func (tx *stubTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return stubResult(0), tx.execErr
}

func (tx *stubTx) QueryRowContext(context.Context, string, ...any) dbpkg.RowScanner {
	return stubRow{err: tx.queryRowErr}
}

type stubRow struct {
	err error
}

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for _, item := range dest {
		value := reflect.ValueOf(item)
		if value.Kind() != reflect.Pointer {
			return errors.New("scan destination must be pointer")
		}
		value.Elem().SetInt(0)
	}
	return nil
}

type stubResult int64

func (r stubResult) LastInsertId() (int64, error) {
	return int64(r), nil
}

func (r stubResult) RowsAffected() (int64, error) {
	return int64(r), nil
}

type stubTicker struct {
	ch      chan time.Time
	stopped bool
}

func (t *stubTicker) C() <-chan time.Time {
	return t.ch
}

func (t *stubTicker) Stop() {
	if t.stopped {
		return
	}
	t.stopped = true
	close(t.ch)
}

func TestEngineClassifiesTransientBeginErrorsAsUnexpectedRuntimeFailures(t *testing.T) {
	registry := newStubRegistry(
		newStubScenario("generic_success", scenario.GenericGroup, nil, nil),
	)
	summary := report.NewSummary(registry.All())
	stopCh := make(chan time.Time, 1)
	beginCh := make(chan struct{}, 1)
	releaseCh := make(chan struct{})
	var beginOnce sync.Once
	session := &stubSession{
		beginErr: errors.New("driver: bad connection"),
		onBegin: func() {
			beginOnce.Do(func() {
				beginCh <- struct{}{}
				<-releaseCh
			})
		},
	}
	engine := NewEngine(EngineConfig{
		Session:        session,
		Registry:       registry,
		Scheduler:      Scheduler{GenericWorkers: 1},
		SeedState:      scenario.SeedState{},
		Summary:        summary,
		Progress:       report.NewProgressReporter(time.Second),
		WarmupDuration: time.Minute,
		After: func(time.Duration) <-chan time.Time {
			return stopCh
		},
		NewTicker: func(time.Duration) ticker {
			return &stubTicker{ch: make(chan time.Time)}
		},
		Now: func() time.Time {
			return time.Date(2026, time.May, 25, 13, 0, 0, 0, time.UTC)
		},
	})

	done := make(chan error, 1)
	go func() {
		done <- engine.Run(context.Background())
	}()

	<-beginCh
	stopCh <- time.Date(2026, time.May, 25, 13, 0, 1, 0, time.UTC)
	close(releaseCh)

	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	stats, ok := summary.Scenario("generic_success")
	if !ok {
		t.Fatal("Scenario(generic_success) not found")
	}
	if stats.Executed < 1 {
		t.Fatalf("Executed = %d, want at least 1", stats.Executed)
	}
	if stats.UnexpectedFailure != stats.Executed {
		t.Fatalf("UnexpectedFailure = %d, want %d", stats.UnexpectedFailure, stats.Executed)
	}
	if stats.LastErrorText != "driver: bad connection" {
		t.Fatalf("LastErrorText = %q, want %q", stats.LastErrorText, "driver: bad connection")
	}
	if got := summary.Totals(); got.Executed != stats.Executed || got.UnexpectedFailure != stats.UnexpectedFailure {
		t.Fatalf("Totals() = %#v, want executed/unexpected to match scenario stats %#v", got, stats)
	}
	if result := model.Classify("", errors.New("driver: bad connection")); result.Kind != model.InfraOrUpgradeTransient {
		t.Fatalf("Classify(driver: bad connection).Kind = %q, want %q", result.Kind, model.InfraOrUpgradeTransient)
	}
}
