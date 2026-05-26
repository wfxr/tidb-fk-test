package config

import (
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.GenericWorkers != 4 {
		t.Fatalf("GenericWorkers = %d, want 4", cfg.GenericWorkers)
	}
	if cfg.BillingWorkers != 3 {
		t.Fatalf("BillingWorkers = %d, want 3", cfg.BillingWorkers)
	}
	if cfg.FailureProbeWorkers != 1 {
		t.Fatalf("FailureProbeWorkers = %d, want 1", cfg.FailureProbeWorkers)
	}
	if cfg.RunDuration != 15*time.Minute {
		t.Fatalf("RunDuration = %v, want 15m", cfg.RunDuration)
	}
	if cfg.ProgressReportInterval != 10*time.Second {
		t.Fatalf("ProgressReportInterval = %v, want 10s", cfg.ProgressReportInterval)
	}
	if cfg.SeedParentRowsPerTable != 1000 {
		t.Fatalf("SeedParentRowsPerTable = %d, want 1000", cfg.SeedParentRowsPerTable)
	}
	if cfg.SeedHotParentKeys != 16 {
		t.Fatalf("SeedHotParentKeys = %d, want 16", cfg.SeedHotParentKeys)
	}
}
