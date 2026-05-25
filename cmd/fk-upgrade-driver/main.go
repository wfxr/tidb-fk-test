package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"
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

	db, err := dbpkg.OpenTiDBCluster(ctx, cfg.DSN)
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
		Session:   db,
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
	printWarmupSummary(os.Stdout, warmupSummary, runtimeSummary.Scenarios())

	if !cfg.CheckerEnabled {
		return nil
	}

	if err := syncProbeSummary(ctx, db, runtimeSummary); err != nil {
		return err
	}

	checkSummary, err := checker.Run(ctx, checkerQueryer{Queryer: db}, applied, runtimeSummary)
	if err != nil {
		return err
	}

	printCheckerSummary(os.Stdout, checkSummary)

	return nil
}

func printWarmupSummary(out *os.File, snapshot report.Snapshot, scenarios []report.ScenarioSummary) {
	expectedOutcomes := snapshot.Success + snapshot.ExpectedFailure

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Warmup Summary")
	fmt.Fprintln(out, "==============")
	fmt.Fprintf(out, "Phase: %s\n", snapshot.Phase)
	fmt.Fprintf(out, "Workers: %d\n", snapshot.ActiveWorkers)
	fmt.Fprintf(out, "Executed: %d\n", snapshot.TotalExecuted)
	fmt.Fprintf(out, "Expected outcomes: %d\n", expectedOutcomes)
	fmt.Fprintf(out, "Success: %d\n", snapshot.Success)
	fmt.Fprintf(out, "Expected failures: %d\n", snapshot.ExpectedFailure)
	fmt.Fprintf(out, "Unexpected failures: %d\n", snapshot.UnexpectedFailure)

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Scenario\tExecuted\tSuccess\tFail\tUnexpected\tLast Error")
	for _, item := range scenarios {
		if item.Executed == 0 && item.ExpectedFailure == 0 && item.UnexpectedFailure == 0 {
			continue
		}
		lastError := item.LastErrorText
		if lastError == "" {
			lastError = "-"
		}
		failures := item.ExpectedFailure + item.UnexpectedFailure
		fmt.Fprintf(
			w,
			"%s\t%d\t%d\t%d\t%d\t%s\n",
			item.Name,
			item.Executed,
			item.Success,
			failures,
			item.UnexpectedFailure,
			lastError,
		)
	}
	_ = w.Flush()
}

func printCheckerSummary(out *os.File, summary checker.Summary) {
	expectedOutcomes := summary.Runtime.Totals.Success + summary.Runtime.Totals.ExpectedFailure

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Checker Summary")
	fmt.Fprintln(out, "===============")
	fmt.Fprintf(out, "Runtime executed: %d\n", summary.Runtime.Totals.Executed)
	fmt.Fprintf(out, "Runtime expected outcomes: %d\n", expectedOutcomes)
	fmt.Fprintf(out, "Runtime success: %d\n", summary.Runtime.Totals.Success)
	fmt.Fprintf(out, "Runtime expected failures: %d\n", summary.Runtime.Totals.ExpectedFailure)
	fmt.Fprintf(out, "Runtime unexpected failures: %d\n", summary.Runtime.Totals.UnexpectedFailure)

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Check\tPassed\tCount\tExpected\tUnexpected")
	for _, item := range summary.Outcomes {
		fmt.Fprintf(
			w,
			"%s\t%t\t%d\t%d\t%d\n",
			item.Name,
			item.Passed,
			item.Count,
			item.ExpectedFKFailure,
			item.UnexpectedFailure,
		)
	}
	_ = w.Flush()
}

func syncProbeSummary(ctx context.Context, db dbpkg.Execer, runtime *report.Summary) error {
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

type checkerQueryer struct {
	dbpkg.Queryer
}

func (q checkerQueryer) QueryRowContext(ctx context.Context, query string, args ...any) checker.RowScanner {
	return q.Queryer.QueryRowContext(ctx, query, args...)
}
