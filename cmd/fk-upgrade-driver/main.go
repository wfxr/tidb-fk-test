package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"log/slog"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/checker"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"
	dbpkg "github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/db"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/report"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/runner"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/scenario"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/seed"
)

const enableSharedLockFKCheckSQL = "SET SESSION tidb_foreign_key_check_in_shared_lock = 1"

func main() {
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	if err := run(context.Background(), *configPath, time.Now()); err != nil {
		slog.Error("fk upgrade driver failed", "path", *configPath, "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, configPath string, now time.Time) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	db, err := openDB(ctx, cfg.DSN)
	if err != nil {
		return err
	}
	defer db.Close()

	applied, err := seed.ApplyAll(ctx, db, cfg)
	if err != nil {
		return err
	}

	registry := scenario.NewRegistry()
	scenarios := registry.All()
	runtimeSummary := report.NewSummary(scenarios)
	scheduler := runner.NewScheduler(cfg)
	progressReporter := report.NewProgressReporter(cfg.ProgressReportInterval)
	initialSnapshot := progressReporter.BuildSnapshot("warmup", scheduler.TotalWorkers(), runtimeSummary, now)

	slog.Info(
		"starting fk upgrade driver skeleton",
		"config_path", configPath,
		"seed_phases", applied.CompletedPhases,
		"total_workers", scheduler.TotalWorkers(),
		"progress_report_interval", progressReporter.Interval(),
		"warmup_duration", cfg.WarmupDuration,
		"post_upgrade_duration", cfg.PostUpgradeDuration,
		"initial_phase", initialSnapshot.Phase,
		"initial_executed", initialSnapshot.TotalExecuted,
		"scenario_count", len(scenarios),
		"checker_enabled", cfg.CheckerEnabled,
	)

	engine := runner.NewEngine(runner.EngineConfig{
		Session:   sqlSession{DB: db},
		Registry:  registry,
		Scheduler: scheduler,
		SeedState: scenario.SeedState{
			Generic:    applied.Generic,
			PropertyMe: applied.PropertyMe,
		},
		Summary:        runtimeSummary,
		Progress:       progressReporter,
		WarmupDuration: cfg.WarmupDuration,
		Now:            time.Now,
		OnProgress: func(snapshot report.Snapshot) {
			slog.Info(
				"warmup progress snapshot",
				"phase", snapshot.Phase,
				"active_workers", snapshot.ActiveWorkers,
				"total_executed", snapshot.TotalExecuted,
				"success", snapshot.Success,
				"expected_failure", snapshot.ExpectedFailure,
				"unexpected_failure", snapshot.UnexpectedFailure,
				"timestamp", snapshot.Timestamp,
			)
		},
	})
	if err := engine.Run(ctx); err != nil {
		return err
	}

	warmupSummary := progressReporter.BuildSnapshot(runner.WarmupPhase, scheduler.TotalWorkers(), runtimeSummary, time.Now())
	slog.Info(
		"bounded warmup completed",
		"phase", warmupSummary.Phase,
		"total_workers", warmupSummary.ActiveWorkers,
		"executed", warmupSummary.TotalExecuted,
		"success", warmupSummary.Success,
		"expected_failure", warmupSummary.ExpectedFailure,
		"unexpected_failure", warmupSummary.UnexpectedFailure,
		"runtime_scenarios", runtimeSummary.Scenarios(),
	)

	if !cfg.CheckerEnabled {
		return nil
	}

	if err := syncProbeSummary(ctx, db, runtimeSummary); err != nil {
		return err
	}

	checkSummary, err := checker.Run(ctx, checkerDB{DB: db}, applied, runtimeSummary)
	if err != nil {
		return err
	}

	slog.Info(
		"checker summary",
		"runtime_totals", checkSummary.Runtime.Totals,
		"runtime_scenarios", checkSummary.Runtime.Scenarios,
		"checks", checkSummary.Outcomes,
	)

	return nil
}

func syncProbeSummary(ctx context.Context, db *sql.DB, runtime *report.Summary) error {
	if runtime == nil {
		return nil
	}

	stats, ok := runtime.Scenario("payment_bill_update_probe")
	if !ok {
		return nil
	}

	_, err := db.ExecContext(
		ctx,
		"INSERT INTO probe_summary (singleton_id, expected_fk_failure, unexpected_failure) VALUES (1, ?, ?) ON DUPLICATE KEY UPDATE expected_fk_failure = VALUES(expected_fk_failure), unexpected_failure = VALUES(unexpected_failure)",
		stats.ExpectedFailure,
		stats.UnexpectedFailure,
	)
	return err
}

func openDB(ctx context.Context, dsn string) (*sql.DB, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("dsn is required")
	}
	if !sqlDriverRegistered("mysql") {
		return nil, errors.New("mysql driver is not registered")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func sqlDriverRegistered(name string) bool {
	for _, driverName := range sql.Drivers() {
		if driverName == name {
			return true
		}
	}
	return false
}

type checkerDB struct {
	DB *sql.DB
}

func (db checkerDB) QueryRowContext(ctx context.Context, query string, args ...any) checker.RowScanner {
	return db.DB.QueryRowContext(ctx, query, args...)
}

type sqlSession struct {
	DB *sql.DB
}

func (db sqlSession) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbpkg.Tx, error) {
	tx, err := db.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, enableSharedLockFKCheckSQL); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return sqlTx{Tx: tx}, nil
}

type sqlTx struct {
	Tx *sql.Tx
}

func (tx sqlTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, query, args...)
}

func (tx sqlTx) QueryRowContext(ctx context.Context, query string, args ...any) dbpkg.RowScanner {
	return tx.Tx.QueryRowContext(ctx, query, args...)
}

func (tx sqlTx) Commit() error {
	return tx.Tx.Commit()
}

func (tx sqlTx) Rollback() error {
	return tx.Tx.Rollback()
}
