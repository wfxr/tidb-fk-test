package runner

import (
	"testing"
	"time"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/config"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/scenario"
)

func TestNewSchedulerBuildsFixedWorkerGroups(t *testing.T) {
	cfg := config.Default()

	scheduler := NewScheduler(cfg)

	if scheduler.ProgressInterval != cfg.ProgressReportInterval {
		t.Fatalf("ProgressInterval = %v, want %v", scheduler.ProgressInterval, cfg.ProgressReportInterval)
	}
	if scheduler.TotalWorkers() != cfg.GenericWorkers+cfg.BillingWorkers+cfg.FailureProbeWorkers {
		t.Fatalf(
			"TotalWorkers() = %d, want %d",
			scheduler.TotalWorkers(),
			cfg.GenericWorkers+cfg.BillingWorkers+cfg.FailureProbeWorkers,
		)
	}

	workers := scheduler.Workers()
	if len(workers) != cfg.GenericWorkers+cfg.BillingWorkers+cfg.FailureProbeWorkers {
		t.Fatalf(
			"len(Workers()) = %d, want %d",
			len(workers),
			cfg.GenericWorkers+cfg.BillingWorkers+cfg.FailureProbeWorkers,
		)
	}

	wantGroups := []scenario.Group{
		scenario.GenericGroup,
		scenario.GenericGroup,
		scenario.GenericGroup,
		scenario.GenericGroup,
		scenario.BillingGroup,
		scenario.BillingGroup,
		scenario.BillingGroup,
		scenario.FailureProbeGroup,
	}
	for i, want := range wantGroups {
		if workers[i].Group != want {
			t.Fatalf("Workers()[%d].Group = %q, want %q", i, workers[i].Group, want)
		}
		if workers[i].ID != i {
			t.Fatalf("Workers()[%d].ID = %d, want %d", i, workers[i].ID, i)
		}
	}
}

func TestNewSchedulerPreservesConfiguredCounts(t *testing.T) {
	cfg := config.Default()
	cfg.GenericWorkers = 2
	cfg.BillingWorkers = 1
	cfg.FailureProbeWorkers = 1
	cfg.ProgressReportInterval = 15 * time.Second

	scheduler := NewScheduler(cfg)

	if scheduler.GenericWorkers != 2 {
		t.Fatalf("GenericWorkers = %d, want 2", scheduler.GenericWorkers)
	}
	if scheduler.BillingWorkers != 1 {
		t.Fatalf("BillingWorkers = %d, want 1", scheduler.BillingWorkers)
	}
	if scheduler.FailureProbeWorkers != 1 {
		t.Fatalf("FailureProbeWorkers = %d, want 1", scheduler.FailureProbeWorkers)
	}
	if scheduler.ProgressInterval != 15*time.Second {
		t.Fatalf("ProgressInterval = %v, want 15s", scheduler.ProgressInterval)
	}
}
