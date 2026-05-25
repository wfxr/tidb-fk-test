package main

import (
	"flag"
	"log/slog"
	"os"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"
)

func main() {
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "path", *configPath, "error", err)
		os.Exit(1)
	}

	slog.Info(
		"starting fk upgrade driver",
		"config_path", *configPath,
		"total_workers", cfg.TotalWorkers,
		"progress_report_interval", cfg.ProgressReportInterval,
	)
}
