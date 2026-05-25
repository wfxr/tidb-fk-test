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
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/report"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/runner"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/scenario"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/seed"
)

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
	runtimeSummary := report.NewSummary(registry.All())
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
	)

	if !cfg.CheckerEnabled {
		return nil
	}

	checkSummary, err := checker.Run(ctx, checkerDB{DB: db}, applied, runtimeSummary)
	if err != nil {
		return err
	}

	slog.Info(
		"checker summary",
		"runtime_executed", checkSummary.Runtime.Executed,
		"checks", checkSummary.Outcomes,
	)

	return nil
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
