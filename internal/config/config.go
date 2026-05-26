package config

import "time"

type Config struct {
	DSN                    []string
	GenericWorkers         int
	BillingWorkers         int
	FailureProbeWorkers    int
	RunDuration            time.Duration
	ProgressReportInterval time.Duration
	SeedParentRowsPerTable int
	SeedHotParentKeys      int
}

func Default() Config {
	return Config{
		GenericWorkers:         4,
		BillingWorkers:         3,
		FailureProbeWorkers:    1,
		RunDuration:            15 * time.Minute,
		ProgressReportInterval: 10 * time.Second,
		SeedParentRowsPerTable: 1000,
		SeedHotParentKeys:      16,
	}
}
