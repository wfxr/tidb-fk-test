package runner

import (
	"time"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/scenario"
)

type Worker struct {
	ID    int            `json:"id"`
	Group scenario.Group `json:"group"`
}

type Scheduler struct {
	GenericWorkers      int
	PropertyMeWorkers   int
	FailureProbeWorkers int
	ProgressInterval    time.Duration
}

func NewScheduler(cfg config.Config) Scheduler {
	return Scheduler{
		GenericWorkers:      cfg.GenericWorkers,
		PropertyMeWorkers:   cfg.PropertyMeWorkers,
		FailureProbeWorkers: cfg.FailureProbeWorkers,
		ProgressInterval:    cfg.ProgressReportInterval,
	}
}

func (s Scheduler) TotalWorkers() int {
	return s.GenericWorkers + s.PropertyMeWorkers + s.FailureProbeWorkers
}

func (s Scheduler) Workers() []Worker {
	workers := make([]Worker, 0, s.TotalWorkers())
	nextID := 0

	appendGroup := func(group scenario.Group, count int) {
		for i := 0; i < count; i++ {
			workers = append(workers, Worker{
				ID:    nextID,
				Group: group,
			})
			nextID++
		}
	}

	appendGroup(scenario.GenericGroup, s.GenericWorkers)
	appendGroup(scenario.PropertyMeGroup, s.PropertyMeWorkers)
	appendGroup(scenario.FailureProbeGroup, s.FailureProbeWorkers)

	return workers
}
