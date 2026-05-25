package runner

import (
	"testing"
	"time"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/scenario"
)

func TestNewSchedulerBuildsFixedWorkerGroups(t *testing.T) {
	cfg := config.Default()

	scheduler := NewScheduler(cfg)

	if scheduler.ProgressInterval != cfg.ProgressReportInterval {
		t.Fatalf("ProgressInterval = %v, want %v", scheduler.ProgressInterval, cfg.ProgressReportInterval)
	}
	if scheduler.TotalWorkers() != cfg.TotalWorkers {
		t.Fatalf("TotalWorkers() = %d, want %d", scheduler.TotalWorkers(), cfg.TotalWorkers)
	}

	workers := scheduler.Workers()
	if len(workers) != cfg.TotalWorkers {
		t.Fatalf("len(Workers()) = %d, want %d", len(workers), cfg.TotalWorkers)
	}

	wantGroups := []scenario.Group{
		scenario.GenericGroup,
		scenario.GenericGroup,
		scenario.GenericGroup,
		scenario.GenericGroup,
		scenario.PropertyMeGroup,
		scenario.PropertyMeGroup,
		scenario.PropertyMeGroup,
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
	cfg.PropertyMeWorkers = 1
	cfg.FailureProbeWorkers = 1
	cfg.TotalWorkers = 4
	cfg.ProgressReportInterval = 15 * time.Second

	scheduler := NewScheduler(cfg)

	if scheduler.GenericWorkers != 2 {
		t.Fatalf("GenericWorkers = %d, want 2", scheduler.GenericWorkers)
	}
	if scheduler.PropertyMeWorkers != 1 {
		t.Fatalf("PropertyMeWorkers = %d, want 1", scheduler.PropertyMeWorkers)
	}
	if scheduler.FailureProbeWorkers != 1 {
		t.Fatalf("FailureProbeWorkers = %d, want 1", scheduler.FailureProbeWorkers)
	}
	if scheduler.ProgressInterval != 15*time.Second {
		t.Fatalf("ProgressInterval = %v, want 15s", scheduler.ProgressInterval)
	}
}
