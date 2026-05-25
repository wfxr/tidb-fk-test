package runner

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/db"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/report"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/scenario"
)

const WarmupPhase = "warmup"

type scenarioCatalog interface {
	All() []scenario.Scenario
}

type ticker interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct {
	ticker *time.Ticker
}

func (t realTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t realTicker) Stop() {
	t.ticker.Stop()
}

type EngineConfig struct {
	Session        db.Session
	Registry       scenarioCatalog
	Scheduler      Scheduler
	SeedState      scenario.SeedState
	Summary        *report.Summary
	Progress       report.ProgressReporter
	WarmupDuration time.Duration
	After          func(time.Duration) <-chan time.Time
	NewTicker      func(time.Duration) ticker
	Now            func() time.Time
	OnProgress     func(report.Snapshot)
}

type Engine struct {
	session        db.Session
	registry       scenarioCatalog
	scheduler      Scheduler
	seedState      scenario.SeedState
	summary        *report.Summary
	progress       report.ProgressReporter
	warmupDuration time.Duration
	after          func(time.Duration) <-chan time.Time
	newTicker      func(time.Duration) ticker
	now            func() time.Time
	onProgress     func(report.Snapshot)
	stopRequested  atomic.Bool
}

type workerPlan struct {
	worker    Worker
	scenarios []scenario.Scenario
	next      int
}

func NewEngine(cfg EngineConfig) *Engine {
	engine := &Engine{
		session:        cfg.Session,
		registry:       cfg.Registry,
		scheduler:      cfg.Scheduler,
		seedState:      cfg.SeedState,
		summary:        cfg.Summary,
		progress:       cfg.Progress,
		warmupDuration: cfg.WarmupDuration,
		after:          cfg.After,
		newTicker:      cfg.NewTicker,
		now:            cfg.Now,
		onProgress:     cfg.OnProgress,
	}
	if engine.after == nil {
		engine.after = time.After
	}
	if engine.newTicker == nil {
		engine.newTicker = func(interval time.Duration) ticker {
			return realTicker{ticker: time.NewTicker(interval)}
		}
	}
	if engine.now == nil {
		engine.now = time.Now
	}
	return engine
}

func (e *Engine) Run(ctx context.Context) error {
	e.stopRequested.Store(false)

	if err := e.validate(); err != nil {
		return err
	}

	plans, err := e.buildWorkerPlans()
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	if interval := e.progress.Interval(); interval > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.progressLoop(runCtx, interval, len(plans))
		}()
	}

	for _, plan := range plans {
		wg.Add(1)
		go func(plan workerPlan) {
			defer wg.Done()
			e.workerLoop(runCtx, plan)
		}(plan)
	}

	select {
	case <-ctx.Done():
		e.requestStop(cancel)
		wg.Wait()
		return ctx.Err()
	case <-e.after(e.warmupDuration):
		e.requestStop(cancel)
		wg.Wait()
		return nil
	}
}

func (e *Engine) validate() error {
	switch {
	case e.session == nil:
		return errors.New("runner engine requires a session")
	case e.registry == nil:
		return errors.New("runner engine requires a scenario registry")
	case e.summary == nil:
		return errors.New("runner engine requires a report summary")
	case e.warmupDuration < 0:
		return errors.New("runner engine warmup duration must be non-negative")
	default:
		return nil
	}
}

func (e *Engine) progressLoop(ctx context.Context, interval time.Duration, activeWorkers int) {
	ticker := e.newTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case tickAt, ok := <-ticker.C():
			if !ok {
				return
			}
			e.progress.EmitSnapshot(WarmupPhase, activeWorkers, e.summary, tickAt, e.onProgress)
		}
	}
}

func (e *Engine) workerLoop(ctx context.Context, plan workerPlan) {
	for {
		if e.shouldStop(ctx) {
			return
		}

		e.runWorkerStep(ctx, plan.nextScenario())
	}
}

func (p *workerPlan) nextScenario() scenario.Scenario {
	item := p.scenarios[p.next]
	p.next = (p.next + 1) % len(p.scenarios)
	return item
}

func (e *Engine) runWorkerStep(ctx context.Context, item scenario.Scenario) {
	if e.shouldStop(ctx) {
		return
	}

	meta := item.Meta()
	tx, err := e.session.BeginTx(ctx, nil)
	if err != nil {
		if e.shouldIgnoreStopArtifact(ctx, err) {
			return
		}
		e.summary.RecordClassified(meta, err, e.now())
		return
	}

	if err := item.Run(ctx, tx, e.seedState); err != nil {
		_ = tx.Rollback()
		if e.shouldIgnoreStopArtifact(ctx, err) {
			return
		}
		e.summary.RecordClassified(meta, err, e.now())
		return
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		if e.shouldIgnoreStopArtifact(ctx, err) {
			return
		}
		e.summary.RecordClassified(meta, err, e.now())
		return
	}

	e.summary.RecordClassified(meta, nil, e.now())
}

func (e *Engine) buildWorkerPlans() ([]workerPlan, error) {
	byGroup := make(map[scenario.Group][]scenario.Scenario)
	for _, item := range e.registry.All() {
		meta := item.Meta()
		byGroup[meta.Group] = append(byGroup[meta.Group], item)
	}
	for group := range byGroup {
		sort.Slice(byGroup[group], func(i, j int) bool {
			return byGroup[group][i].Meta().Name < byGroup[group][j].Meta().Name
		})
	}

	workers := e.scheduler.Workers()
	plans := make([]workerPlan, 0, len(workers))
	groupOffsets := make(map[scenario.Group]int)
	for _, worker := range workers {
		items := byGroup[worker.Group]
		if len(items) == 0 {
			return nil, fmt.Errorf("no scenarios registered for worker group %q", worker.Group)
		}
		offset := groupOffsets[worker.Group]
		plans = append(plans, workerPlan{
			worker:    worker,
			scenarios: items,
			next:      offset % len(items),
		})
		groupOffsets[worker.Group] = offset + 1
	}

	return plans, nil
}

func (e *Engine) requestStop(cancel context.CancelFunc) {
	e.stopRequested.Store(true)
	cancel()
}

func (e *Engine) shouldStop(ctx context.Context) bool {
	return e.stopRequested.Load() || ctx.Err() != nil
}

func (e *Engine) shouldIgnoreStopArtifact(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}

	stopRequested := e.stopRequested.Load() || errors.Is(ctx.Err(), context.Canceled)
	if !stopRequested {
		return false
	}

	if errors.Is(err, context.Canceled) {
		return true
	}

	normalized := strings.ToLower(err.Error())
	return strings.Contains(normalized, "context canceled")
}
