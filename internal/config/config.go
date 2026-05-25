package config

import (
	"bytes"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DSN                    []string      `yaml:"dsn"`
	TotalWorkers           int           `yaml:"total_workers"`
	GenericWorkers         int           `yaml:"generic_workers"`
	PropertyMeWorkers      int           `yaml:"propertyme_workers"`
	FailureProbeWorkers    int           `yaml:"failure_probe_workers"`
	GenericWeight          int           `yaml:"generic_weight"`
	PropertyMeWeight       int           `yaml:"propertyme_weight"`
	FailureProbeWeight     int           `yaml:"failure_probe_weight"`
	WarmupDuration         time.Duration `yaml:"warmup_duration"`
	PostUpgradeDuration    time.Duration `yaml:"post_upgrade_duration"`
	FailureProbeInterval   time.Duration `yaml:"failure_probe_interval"`
	ProgressReportInterval time.Duration `yaml:"progress_report_interval"`
	ScenarioTimeout        time.Duration `yaml:"scenario_timeout"`
	MaxRetry               int           `yaml:"max_retry"`
	SeedParentRowsPerTable int           `yaml:"seed_parent_rows_per_table"`
	SeedHotParentKeys      int           `yaml:"seed_hot_parent_keys"`
	CheckerEnabled         bool          `yaml:"checker_enabled"`
}

func Default() Config {
	return Config{
		TotalWorkers:           8,
		GenericWorkers:         4,
		PropertyMeWorkers:      3,
		FailureProbeWorkers:    1,
		GenericWeight:          60,
		PropertyMeWeight:       35,
		FailureProbeWeight:     5,
		WarmupDuration:         15 * time.Minute,
		PostUpgradeDuration:    20 * time.Minute,
		FailureProbeInterval:   10 * time.Second,
		ProgressReportInterval: 10 * time.Second,
		ScenarioTimeout:        30 * time.Second,
		MaxRetry:               0,
		SeedParentRowsPerTable: 1000,
		SeedHotParentKeys:      16,
		CheckerEnabled:         true,
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
