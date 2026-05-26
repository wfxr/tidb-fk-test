package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/seed"
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
		PropertyMeWorkers:      1,
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
