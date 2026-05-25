package report

import "time"

type ProgressReporter struct {
	interval time.Duration
}

func NewProgressReporter(interval time.Duration) ProgressReporter {
	return ProgressReporter{interval: interval}
}

func (r ProgressReporter) Interval() time.Duration {
	return r.interval
}

type Snapshot struct {
	Timestamp         time.Time `json:"timestamp"`
	Phase             string    `json:"phase"`
	ActiveWorkers     int       `json:"active_workers"`
	TotalExecuted     int64     `json:"total_executed"`
	Success           int64     `json:"success"`
	ExpectedFailure   int64     `json:"expected_failure"`
	UnexpectedFailure int64     `json:"unexpected_failure"`
}

func (r ProgressReporter) BuildSnapshot(phase string, activeWorkers int, summary *Summary, now time.Time) Snapshot {
	totals := Totals{}
	if summary != nil {
		totals = summary.Totals()
	}

	return Snapshot{
		Timestamp:         now,
		Phase:             phase,
		ActiveWorkers:     activeWorkers,
		TotalExecuted:     totals.Executed,
		Success:           totals.Success,
		ExpectedFailure:   totals.ExpectedFailure,
		UnexpectedFailure: totals.UnexpectedFailure,
	}
}
