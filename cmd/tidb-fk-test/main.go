package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/checker"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/config"
	dbpkg "github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/db"
	ilog "github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/logging"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/report"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/runner"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/scenario"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/seed"
)

type connectionOptions struct {
	Nodes    string
	User     string
	Password string
	DB       string
}

type prepareOptions struct {
	connectionOptions
	SeedParentRowsPerTable int
	SeedHotParentKeys      int
}

type runOptions struct {
	connectionOptions
	Duration               time.Duration
	ProgressReportInterval time.Duration
	GenericWorkers         int
	BillingWorkers         int
	FailureProbeWorkers    int
}

type clusterConn interface {
	dbpkg.Session
	dbpkg.Execer
	dbpkg.Queryer
	Close() error
}

type preparedMetadataReader interface {
	dbpkg.Queryer
}

type commandDeps struct {
	now                  func() time.Time
	openCluster          func(context.Context, []string) (clusterConn, error)
	prepare              func(context.Context, config.Config, clusterConn) (seed.AppliedState, error)
	run                  func(context.Context, config.Config, time.Time, clusterConn, *slog.Logger) error
	readPreparedMetadata func(context.Context, preparedMetadataReader) (seed.PreparedMetadata, error)
	validatePrepared     func(context.Context, preparedMetadataReader, seed.AppliedState) error
}

var lastErrorLogPath string

const interruptedFinalizeTimeout = 10 * time.Second

func main() {
	os.Exit(runMain(os.Args[1:], defaultCommandDeps()))
}

func runMain(args []string, deps commandDeps) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd := newRootCommand(deps)
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(ctx); err != nil {
		if lastErrorLogPath != "" {
			fmt.Fprintf(os.Stderr, "tidb-fk-test failed; see %s\n", lastErrorLogPath)
		} else {
			fmt.Fprintf(os.Stderr, "tidb-fk-test failed: %v\n", err)
		}
		return 1
	}
	return 0
}

func defaultCommandDeps() commandDeps {
	return commandDeps{
		now: func() time.Time { return time.Now().UTC() },
		openCluster: func(ctx context.Context, dsns []string) (clusterConn, error) {
			return dbpkg.OpenTiDBCluster(ctx, dsns)
		},
		prepare: func(ctx context.Context, cfg config.Config, db clusterConn) (seed.AppliedState, error) {
			return seed.ApplyAll(ctx, db, cfg)
		},
		run: func(ctx context.Context, cfg config.Config, now time.Time, db clusterConn, errorLogger *slog.Logger) error {
			return runWorkload(ctx, cfg, now, db, errorLogger)
		},
		readPreparedMetadata: func(ctx context.Context, reader preparedMetadataReader) (seed.PreparedMetadata, error) {
			return seed.ReadPreparedMetadata(ctx, reader)
		},
		validatePrepared: func(ctx context.Context, reader preparedMetadataReader, applied seed.AppliedState) error {
			return seed.ValidatePreparedState(ctx, reader, applied)
		},
	}
}

func newRootCommand(deps commandDeps) *cobra.Command {
	if deps.now == nil {
		deps.now = func() time.Time { return time.Now().UTC() }
	}

	var legacyConfig string
	rootCmd := &cobra.Command{
		Use:           "tidb-fk-test",
		Short:         "Run the foreign-key workload prepare/run flow",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			if legacyConfig != "" {
				return errors.New("config files are no longer supported; use prepare/run flags")
			}
			return nil
		},
	}
	rootCmd.PersistentFlags().StringVar(&legacyConfig, "config", "", "legacy config file path")
	_ = rootCmd.PersistentFlags().MarkHidden("config")

	rootCmd.AddCommand(newPrepareCommand(deps))
	rootCmd.AddCommand(newRunCommand(deps))
	return rootCmd
}

func newPrepareCommand(deps commandDeps) *cobra.Command {
	cfgDefaults := config.Default()
	opts := prepareOptions{
		SeedParentRowsPerTable: cfgDefaults.SeedParentRowsPerTable,
		SeedHotParentKeys:      cfgDefaults.SeedHotParentKeys,
	}

	cmd := &cobra.Command{
		Use:   "prepare",
		Short: "Create schema, seed fixtures, and record prepared metadata",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := buildPrepareConfig(opts)
			if err != nil {
				return err
			}
			db, err := deps.openCluster(cmd.Context(), cfg.DSN)
			if err != nil {
				return err
			}
			defer db.Close()

			applied, err := deps.prepare(cmd.Context(), cfg, db)
			if err != nil {
				return err
			}
			slog.Info(
				"prepare completed",
				"nodes", len(cfg.DSN),
				"completed_phases", applied.CompletedPhases,
				"seed_plan_version", seed.SeedPlanVersion,
				"seed_parent_rows_per_table", cfg.SeedParentRowsPerTable,
				"seed_hot_parent_keys", cfg.SeedHotParentKeys,
			)
			return nil
		},
	}
	addConnectionFlags(cmd, &opts.connectionOptions)
	cmd.Flags().IntVarP(&opts.SeedParentRowsPerTable, "seed-parent-rows-per-table", "r", opts.SeedParentRowsPerTable, "number of deterministic parent/customer rows to seed")
	cmd.Flags().IntVarP(&opts.SeedHotParentKeys, "seed-hot-parent-keys", "k", opts.SeedHotParentKeys, "number of hot parent keys to reserve for contention scenarios")
	return cmd
}

func newRunCommand(deps commandDeps) *cobra.Command {
	cfgDefaults := config.Default()
	opts := runOptions{
		Duration:               cfgDefaults.RunDuration,
		ProgressReportInterval: cfgDefaults.ProgressReportInterval,
		GenericWorkers:         cfgDefaults.GenericWorkers,
		BillingWorkers:         cfgDefaults.BillingWorkers,
		FailureProbeWorkers:    cfgDefaults.FailureProbeWorkers,
	}

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute workload against a previously prepared database and run checks",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			errorLogPath, errorLogger, logFile, err := newErrorLogger(deps.now())
			if err != nil {
				return err
			}
			defer logFile.Close()
			lastErrorLogPath = errorLogPath
			fmt.Fprintf(cmd.OutOrStdout(), "Error log: %s\n", errorLogPath)

			baseCfg, err := buildRunConfig(opts, seed.PreparedMetadata{})
			if err != nil {
				logCommandError(errorLogger, "build_run_config", err)
				return err
			}
			db, err := deps.openCluster(ctx, baseCfg.DSN)
			if err != nil {
				logCommandError(errorLogger, "open_cluster", err)
				return err
			}
			defer db.Close()

			metadata, err := deps.readPreparedMetadata(ctx, db)
			if err != nil {
				if errors.Is(err, seed.ErrPrepareMetadataNotFound) {
					logCommandError(errorLogger, "read_prepared_metadata", err)
					return fmt.Errorf("prepare metadata not found: run `tidb-fk-test prepare` first")
				}
				logCommandError(errorLogger, "read_prepared_metadata", err)
				return err
			}
			if metadata.SeedPlanVersion != seed.SeedPlanVersion {
				err = fmt.Errorf(
					"prepare metadata seed plan version %d is incompatible with binary version %d; rerun `tidb-fk-test prepare`",
					metadata.SeedPlanVersion,
					seed.SeedPlanVersion,
				)
				logCommandError(errorLogger, "validate_prepare_metadata", err)
				return err
			}

			cfg, err := buildRunConfig(opts, metadata)
			if err != nil {
				logCommandError(errorLogger, "build_run_config", err)
				return err
			}
			applied := seed.BuildAppliedState(cfg)
			if err := deps.validatePrepared(ctx, db, applied); err != nil {
				logCommandError(errorLogger, "validate_prepared_state", err)
				return err
			}
			if err := deps.run(ctx, cfg, deps.now(), db, errorLogger); err != nil {
				logCommandError(errorLogger, "run_workload", err)
				return err
			}
			return nil
		},
	}
	addConnectionFlags(cmd, &opts.connectionOptions)
	cmd.Flags().DurationVarP(&opts.Duration, "duration", "d", opts.Duration, "how long to run workload execution")
	cmd.Flags().DurationVarP(&opts.ProgressReportInterval, "progress-report-interval", "i", opts.ProgressReportInterval, "interval between workload progress snapshots")
	cmd.Flags().IntVarP(&opts.GenericWorkers, "generic-workers", "g", opts.GenericWorkers, "number of generic scenario workers")
	cmd.Flags().IntVarP(&opts.BillingWorkers, "billing-workers", "m", opts.BillingWorkers, "number of Billing scenario workers")
	cmd.Flags().IntVarP(&opts.FailureProbeWorkers, "failure-probe-workers", "f", opts.FailureProbeWorkers, "number of failure probe workers")
	return cmd
}

func addConnectionFlags(cmd *cobra.Command, opts *connectionOptions) {
	cmd.Flags().StringVarP(&opts.Nodes, "nodes", "n", "", "comma-separated TiDB nodes in host:port form")
	cmd.Flags().StringVarP(&opts.User, "user", "u", "root", "database user")
	cmd.Flags().StringVarP(&opts.Password, "password", "p", "", "database password")
	cmd.Flags().StringVarP(&opts.DB, "db", "D", "test", "database name")
	_ = cmd.MarkFlagRequired("nodes")
}

func buildPrepareConfig(opts prepareOptions) (config.Config, error) {
	cfg := config.Default()
	dsns, err := buildDSNs(opts.connectionOptions)
	if err != nil {
		return config.Config{}, err
	}
	cfg.DSN = dsns
	cfg.SeedParentRowsPerTable = opts.SeedParentRowsPerTable
	cfg.SeedHotParentKeys = opts.SeedHotParentKeys
	return cfg, nil
}

func buildRunConfig(opts runOptions, metadata seed.PreparedMetadata) (config.Config, error) {
	cfg := config.Default()
	dsns, err := buildDSNs(opts.connectionOptions)
	if err != nil {
		return config.Config{}, err
	}
	cfg.DSN = dsns
	cfg.RunDuration = opts.Duration
	cfg.ProgressReportInterval = opts.ProgressReportInterval
	cfg.GenericWorkers = opts.GenericWorkers
	cfg.BillingWorkers = opts.BillingWorkers
	cfg.FailureProbeWorkers = opts.FailureProbeWorkers
	if metadata.SeedParentRowsPerTable != 0 {
		cfg.SeedParentRowsPerTable = metadata.SeedParentRowsPerTable
	}
	if metadata.SeedHotParentKeys != 0 {
		cfg.SeedHotParentKeys = metadata.SeedHotParentKeys
	}
	return cfg, nil
}

func buildDSNs(opts connectionOptions) ([]string, error) {
	if strings.TrimSpace(opts.Nodes) == "" {
		return nil, errors.New("--nodes is required")
	}
	if strings.TrimSpace(opts.User) == "" {
		return nil, errors.New("--user is required")
	}
	if strings.TrimSpace(opts.DB) == "" {
		return nil, errors.New("--db is required")
	}

	endpoints, err := parseNodes(opts.Nodes)
	if err != nil {
		return nil, err
	}
	dsns := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		credentials := opts.User
		if opts.Password != "" {
			credentials += ":" + opts.Password
		}
		dsns = append(dsns, fmt.Sprintf("%s@tcp(%s:%d)/%s", credentials, endpoint.Host, endpoint.Port, opts.DB))
	}
	return dsns, nil
}

type nodeEndpoint struct {
	Host string
	Port int
}

func parseNodes(value string) ([]nodeEndpoint, error) {
	parts := strings.Split(value, ",")
	endpoints := make([]nodeEndpoint, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("--nodes contains an empty entry: %q", value)
		}
		host, portText, err := net.SplitHostPort(part)
		if err != nil {
			return nil, fmt.Errorf("--nodes entry %q must be in host:port form", part)
		}
		port, err := strconv.Atoi(portText)
		if err != nil || port <= 0 {
			return nil, fmt.Errorf("--nodes entry %q has invalid port %q", part, portText)
		}
		if strings.TrimSpace(host) == "" {
			return nil, fmt.Errorf("--nodes entry %q has an empty host", part)
		}
		endpoints = append(endpoints, nodeEndpoint{Host: host, Port: port})
	}
	return endpoints, nil
}

func runWorkload(ctx context.Context, cfg config.Config, now time.Time, db clusterConn, errorLogger *slog.Logger) error {
	registry := scenario.NewRegistry()
	scenarios := registry.All()
	runtimeSummary := report.NewSummary(scenarios)
	scheduler := runner.NewScheduler(cfg)
	progressReporter := report.NewProgressReporter(cfg.ProgressReportInterval)
	initialSnapshot := progressReporter.BuildSnapshot(runner.RunPhase, scheduler.TotalWorkers(), runtimeSummary, now)
	applied := seed.BuildAppliedState(cfg)

	slog.Info(
		"starting tidb-fk-test",
		"seed_plan_version", seed.SeedPlanVersion,
		"total_workers", scheduler.TotalWorkers(),
		"progress_report_interval", progressReporter.Interval(),
		"duration", cfg.RunDuration,
		"initial_phase", initialSnapshot.Phase,
		"initial_executed", initialSnapshot.TotalExecuted,
		"scenario_count", len(scenarios),
	)

	engine := runner.NewEngine(runner.EngineConfig{
		Session:   db,
		Registry:  registry,
		Scheduler: scheduler,
		SeedState: scenario.SeedState{
			Generic: applied.Generic,
			Billing: applied.Billing,
		},
		Summary:     runtimeSummary,
		Progress:    progressReporter,
		RunDuration: cfg.RunDuration,
		Now:         time.Now,
		ErrorLogger: errorLogger,
		OnProgress: func(snapshot report.Snapshot) {
			logRunProgressSnapshot(snapshot)
		},
	})
	runErr := engine.Run(ctx)
	runSummary := progressReporter.BuildSnapshot(runner.RunPhase, scheduler.TotalWorkers(), runtimeSummary, time.Now())
	interrupted := errors.Is(runErr, context.Canceled)
	if runErr == nil || interrupted {
		if interrupted {
			printInterruptedRunNotice(os.Stdout)
		}
		printRunSummary(os.Stdout, runSummary, runtimeSummary.Scenarios())
	}
	if runErr != nil {
		if interrupted {
			finalizeCtx, cancel := context.WithTimeout(context.Background(), interruptedFinalizeTimeout)
			defer cancel()
			return runFinalChecks(finalizeCtx, db, applied, runtimeSummary)
		}
		return runErr
	}

	return runFinalChecks(ctx, db, applied, runtimeSummary)
}

func logRunProgressSnapshot(snapshot report.Snapshot) {
	slog.Info(
		"run progress snapshot",
		"phase", snapshot.Phase,
		"active_workers", snapshot.ActiveWorkers,
		"total_executed", snapshot.TotalExecuted,
		"success", snapshot.Success,
		"expected_failure", snapshot.ExpectedFailure,
		"unexpected_failure", snapshot.UnexpectedFailure,
	)
}

func runFinalChecks(ctx context.Context, db clusterConn, applied seed.AppliedState, runtimeSummary *report.Summary) error {
	checkSummary, err := checker.Run(ctx, checkerQueryer{Queryer: db}, applied, runtimeSummary)
	if err != nil {
		return err
	}

	printCheckerSummary(os.Stdout, checkSummary)
	return nil
}

func printInterruptedRunNotice(out io.Writer) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Run interrupted; printing partial summary and running checker with a %s timeout.\n", interruptedFinalizeTimeout)
}

func printRunSummary(out io.Writer, snapshot report.Snapshot, scenarios []report.ScenarioSummary) {
	expectedOutcomes := snapshot.Success + snapshot.ExpectedFailure

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Run Summary")
	fmt.Fprintln(out, "===========")
	fmt.Fprintf(out, "Phase: %s\n", snapshot.Phase)
	fmt.Fprintf(out, "Workers: %d\n", snapshot.ActiveWorkers)
	fmt.Fprintf(out, "Executed: %d\n", snapshot.TotalExecuted)
	fmt.Fprintf(out, "Expected outcomes: %d\n", expectedOutcomes)
	fmt.Fprintf(out, "Success: %d\n", snapshot.Success)
	fmt.Fprintf(out, "Expected failures: %d\n", snapshot.ExpectedFailure)
	fmt.Fprintf(out, "Unexpected failures: %d\n", snapshot.UnexpectedFailure)

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Scenario\tExecuted\tSuccess\tFail\tUnexpected")
	for _, item := range scenarios {
		if item.Executed == 0 && item.ExpectedFailure == 0 && item.UnexpectedFailure == 0 {
			continue
		}
		failures := item.ExpectedFailure + item.UnexpectedFailure
		fmt.Fprintf(
			w,
			"%s\t%d\t%d\t%d\t%d\n",
			item.Name,
			item.Executed,
			item.Success,
			failures,
			item.UnexpectedFailure,
		)
	}
	_ = w.Flush()
}

func printCheckerSummary(out io.Writer, summary checker.Summary) {
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
	fmt.Fprintln(w, "Check\tCount\tPassed")
	for _, item := range summary.Outcomes {
		fmt.Fprintf(
			w,
			"%s\t%d\t%t\n",
			item.Name,
			item.Count,
			item.Passed,
		)
	}
	_ = w.Flush()
}

type checkerQueryer struct {
	dbpkg.Queryer
}

func (q checkerQueryer) QueryRowContext(ctx context.Context, query string, args ...any) checker.RowScanner {
	return q.Queryer.QueryRowContext(ctx, query, args...)
}

func newErrorLogger(now time.Time) (string, *slog.Logger, *os.File, error) {
	if err := os.MkdirAll("logs", 0o755); err != nil {
		return "", nil, nil, err
	}
	path := filepath.Join("logs", fmt.Sprintf("error-%d.log", now.Unix()))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", nil, nil, err
	}
	return path, ilog.NewJSONLogger(file), file, nil
}

func logCommandError(logger *slog.Logger, step string, err error) {
	if logger == nil || err == nil {
		return
	}
	logger.Error("command_error", "phase", runner.RunPhase, "step_name", step, "error_text", err.Error())
}
