package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/checker"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/config"
	dbpkg "github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/db"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/report"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/seed"
)

func TestBuildDSNsParsesMultipleNodes(t *testing.T) {
	got, err := buildDSNs(connectionOptions{
		Nodes:    "tidb-a:4000,tidb-b:4001",
		User:     "root",
		Password: "secret",
		DB:       "test",
	})
	if err != nil {
		t.Fatalf("buildDSNs() error = %v", err)
	}

	want := []string{
		"root:secret@tcp(tidb-a:4000)/test",
		"root:secret@tcp(tidb-b:4001)/test",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("buildDSNs() = %v, want %v", got, want)
	}
}

func TestBuildDSNsRejectsMalformedNodes(t *testing.T) {
	_, err := buildDSNs(connectionOptions{
		Nodes: "tidb-a,tidb-b:4001",
		User:  "root",
		DB:    "test",
	})
	if err == nil || !strings.Contains(err.Error(), "--nodes") {
		t.Fatalf("buildDSNs() error = %v, want malformed --nodes error", err)
	}
}

func TestBuildDSNsUsesDefaultUserAndDB(t *testing.T) {
	got, err := buildDSNs(connectionOptions{
		Nodes: "tidb-a:4000",
		User:  "root",
		DB:    "test",
	})
	if err != nil {
		t.Fatalf("buildDSNs() error = %v", err)
	}
	if len(got) != 1 || got[0] != "root@tcp(tidb-a:4000)/test" {
		t.Fatalf("buildDSNs() = %v, want default root/test DSN", got)
	}
}

func TestBuildPrepareConfigUsesSeedFlagsAndDefaults(t *testing.T) {
	cfg, err := buildPrepareConfig(prepareOptions{
		connectionOptions: connectionOptions{
			Nodes: "127.0.0.1:4000",
			User:  "root",
			DB:    "test",
		},
		SeedParentRowsPerTable: 42,
		SeedHotParentKeys:      7,
	})
	if err != nil {
		t.Fatalf("buildPrepareConfig() error = %v", err)
	}

	if cfg.SeedParentRowsPerTable != 42 {
		t.Fatalf("SeedParentRowsPerTable = %d, want 42", cfg.SeedParentRowsPerTable)
	}
	if cfg.SeedHotParentKeys != 7 {
		t.Fatalf("SeedHotParentKeys = %d, want 7", cfg.SeedHotParentKeys)
	}
	if len(cfg.DSN) != 1 || cfg.DSN[0] != "root@tcp(127.0.0.1:4000)/test" {
		t.Fatalf("DSN = %v, want single node DSN", cfg.DSN)
	}
}

func TestBuildRunConfigUsesPreparedMetadataForSeedInputs(t *testing.T) {
	cfg, err := buildRunConfig(runOptions{
		connectionOptions: connectionOptions{
			Nodes: "127.0.0.1:4000",
			User:  "root",
			DB:    "test",
		},
		Duration:               3 * time.Second,
		ProgressReportInterval: time.Second,
		GenericWorkers:         2,
		BillingWorkers:         1,
		FailureProbeWorkers:    1,
	}, seed.PreparedMetadata{
		SeedPlanVersion:        seed.SeedPlanVersion,
		SeedParentRowsPerTable: 11,
		SeedHotParentKeys:      4,
		PreparedAt:             time.Date(2026, time.May, 25, 15, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("buildRunConfig() error = %v", err)
	}

	if cfg.SeedParentRowsPerTable != 11 {
		t.Fatalf("SeedParentRowsPerTable = %d, want 11", cfg.SeedParentRowsPerTable)
	}
	if cfg.SeedHotParentKeys != 4 {
		t.Fatalf("SeedHotParentKeys = %d, want 4", cfg.SeedHotParentKeys)
	}
	if cfg.RunDuration != 3*time.Second {
		t.Fatalf("RunDuration = %v, want 3s", cfg.RunDuration)
	}
}

func TestRootCommandRejectsLegacyConfigFlag(t *testing.T) {
	deps := commandDeps{
		now: func() time.Time { return time.Date(2026, time.May, 25, 15, 30, 0, 0, time.UTC) },
		prepare: func(context.Context, config.Config, clusterConn) (seed.AppliedState, error) {
			t.Fatal("prepare should not run")
			return seed.AppliedState{}, nil
		},
		run: func(context.Context, config.Config, time.Time, clusterConn, *slog.Logger) error {
			t.Fatal("run should not run")
			return nil
		},
		readPreparedMetadata: func(context.Context, preparedMetadataReader) (seed.PreparedMetadata, error) {
			t.Fatal("readPreparedMetadata should not run")
			return seed.PreparedMetadata{}, nil
		},
	}

	cmd := newRootCommand(deps)
	cmd.SetArgs([]string{"run", "--config", "configs/default.yaml"})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "config files are no longer supported") {
		t.Fatalf("Execute() error = %v, want legacy config rejection", err)
	}
}

func TestAddConnectionFlagsDefaultsUserAndDB(t *testing.T) {
	cmd := newPrepareCommand(commandDeps{})
	if got, err := cmd.Flags().GetString("user"); err != nil || got != "root" {
		t.Fatalf("user default = %q, %v, want root", got, err)
	}
	if got, err := cmd.Flags().GetString("db"); err != nil || got != "test" {
		t.Fatalf("db default = %q, %v, want test", got, err)
	}
}

func TestPrepareCommandRegistersConnectionShorthands(t *testing.T) {
	cmd := newPrepareCommand(commandDeps{})

	if got := cmd.Flag("nodes").Shorthand; got != "n" {
		t.Fatalf("nodes shorthand = %q, want n", got)
	}
	if got := cmd.Flag("user").Shorthand; got != "u" {
		t.Fatalf("user shorthand = %q, want u", got)
	}
	if got := cmd.Flag("password").Shorthand; got != "p" {
		t.Fatalf("password shorthand = %q, want p", got)
	}
	if got := cmd.Flag("db").Shorthand; got != "D" {
		t.Fatalf("db shorthand = %q, want D", got)
	}
}

func TestRunCommandRegistersCoreShorthands(t *testing.T) {
	cmd := newRunCommand(commandDeps{})

	if got := cmd.Flag("duration").Shorthand; got != "d" {
		t.Fatalf("duration shorthand = %q, want d", got)
	}
	if got := cmd.Flag("progress-report-interval").Shorthand; got != "i" {
		t.Fatalf("progress-report-interval shorthand = %q, want i", got)
	}
}

func TestPrintCheckerSummaryOrdersColumnsAsCheckCountPassed(t *testing.T) {
	var buf bytes.Buffer

	printCheckerSummary(&buf, checker.Summary{
		Outcomes: []checker.Outcome{
			{
				Name:              "generic_orphan_child",
				Passed:            true,
				Count:             7,
				UnexpectedFailure: 2,
			},
		},
	})

	output := buf.String()
	lines := strings.Split(output, "\n")
	var headerLine string
	var rowLine string
	for _, line := range lines {
		if strings.Contains(line, "Check") && strings.Contains(line, "Count") && strings.Contains(line, "Passed") {
			headerLine = line
		}
		if strings.Contains(line, "generic_orphan_child") {
			rowLine = line
		}
	}

	if got := strings.Fields(headerLine); strings.Join(got, ",") != "Check,Count,Passed" {
		t.Fatalf("header fields = %v, want [Check Count Passed]", got)
	}
	if got := strings.Fields(rowLine); strings.Join(got, ",") != "generic_orphan_child,7,true" {
		t.Fatalf("row fields = %v, want [generic_orphan_child 7 true]", got)
	}
}

func TestRunWorkloadPrintsSummaryWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := config.Default()
	cfg.RunDuration = time.Minute
	cfg.ProgressReportInterval = 0

	db := &stubRunClusterConn{}
	output := captureStdout(t, func() {
		err := runWorkload(
			ctx,
			cfg,
			time.Date(2026, time.May, 26, 10, 0, 0, 0, time.UTC),
			db,
			slog.New(slog.NewTextHandler(io.Discard, nil)),
		)
		if err != nil {
			t.Fatalf("runWorkload() error = %v, want nil", err)
		}
	})

	if !strings.Contains(output, "Run Summary") {
		t.Fatalf("stdout = %q, want Run Summary", output)
	}
	if !strings.Contains(output, "Run interrupted") {
		t.Fatalf("stdout = %q, want interruption notice", output)
	}
	if !strings.Contains(output, "Checker Summary") {
		t.Fatalf("stdout = %q, want checker summary after interruption", output)
	}
	if db.beginCalls != 0 {
		t.Fatalf("BeginTx calls = %d, want 0", db.beginCalls)
	}
	if db.execCalls != 0 {
		t.Fatalf("ExecContext calls = %d, want 0", db.execCalls)
	}
	if db.queryCalls != 2 {
		t.Fatalf("QueryRowContext calls = %d, want 2", db.queryCalls)
	}
	if db.canceledExecCalls != 0 {
		t.Fatalf("ExecContext canceled calls = %d, want 0", db.canceledExecCalls)
	}
	if db.canceledQueryCalls != 0 {
		t.Fatalf("QueryRowContext canceled calls = %d, want 0", db.canceledQueryCalls)
	}
}

func TestLogRunProgressSnapshotOmitsDuplicateTimestampField(t *testing.T) {
	var buf bytes.Buffer
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(oldDefault)

	logRunProgressSnapshot(report.Snapshot{
		Timestamp:         time.Date(2026, time.May, 26, 12, 0, 1, 0, time.UTC),
		Phase:             "run",
		ActiveWorkers:     3,
		TotalExecuted:     7,
		Success:           5,
		ExpectedFailure:   1,
		UnexpectedFailure: 1,
	})

	output := buf.String()
	if !strings.Contains(output, "run progress snapshot") {
		t.Fatalf("output = %q, want progress log message", output)
	}
	if strings.Contains(output, "timestamp=") {
		t.Fatalf("output = %q, want no duplicate timestamp field", output)
	}
}

type stubRunClusterConn struct {
	beginCalls         int
	execCalls          int
	queryCalls         int
	canceledExecCalls  int
	canceledQueryCalls int
}

func (s *stubRunClusterConn) BeginTx(context.Context, *sql.TxOptions) (dbpkg.Tx, error) {
	s.beginCalls++
	return nil, context.Canceled
}

func (s *stubRunClusterConn) ExecContext(ctx context.Context, _ string, _ ...any) (sql.Result, error) {
	s.execCalls++
	if ctx.Err() != nil {
		s.canceledExecCalls++
	}
	return nil, nil
}

func (s *stubRunClusterConn) QueryRowContext(ctx context.Context, _ string, _ ...any) dbpkg.RowScanner {
	s.queryCalls++
	if ctx.Err() != nil {
		s.canceledQueryCalls++
	}
	return stubRunRow{}
}

func (s *stubRunClusterConn) Close() error {
	return nil
}

type stubRunRow struct{}

func (stubRunRow) Scan(...any) error {
	return nil
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := writePipe.Close(); err != nil {
		t.Fatalf("writePipe.Close() error = %v", err)
	}
	output, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if err := readPipe.Close(); err != nil {
		t.Fatalf("readPipe.Close() error = %v", err)
	}
	return string(output)
}
