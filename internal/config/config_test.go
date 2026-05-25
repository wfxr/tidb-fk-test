package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.TotalWorkers != 8 {
		t.Fatalf("TotalWorkers = %d, want 8", cfg.TotalWorkers)
	}
	if cfg.ProgressReportInterval != 10*time.Second {
		t.Fatalf("ProgressReportInterval = %v, want 10s", cfg.ProgressReportInterval)
	}
	if cfg.FailureProbeInterval != 10*time.Second {
		t.Fatalf("FailureProbeInterval = %v, want 10s", cfg.FailureProbeInterval)
	}
}

func TestLoadOverridesAndPreservesDefaults(t *testing.T) {
	path := writeTempConfig(t, "total_workers: 12\nscenario_timeout: 45s\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.TotalWorkers != 12 {
		t.Fatalf("TotalWorkers = %d, want 12", cfg.TotalWorkers)
	}
	if cfg.ScenarioTimeout != 45*time.Second {
		t.Fatalf("ScenarioTimeout = %v, want 45s", cfg.ScenarioTimeout)
	}
	if cfg.ProgressReportInterval != 10*time.Second {
		t.Fatalf("ProgressReportInterval = %v, want 10s", cfg.ProgressReportInterval)
	}
	if cfg.CheckerEnabled != true {
		t.Fatalf("CheckerEnabled = %v, want true", cfg.CheckerEnabled)
	}
}

func TestLoadHonorsExplicitZeroAndFalseOverrides(t *testing.T) {
	path := writeTempConfig(t, "generic_workers: 0\nchecker_enabled: false\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.GenericWorkers != 0 {
		t.Fatalf("GenericWorkers = %d, want 0", cfg.GenericWorkers)
	}
	if cfg.CheckerEnabled != false {
		t.Fatalf("CheckerEnabled = %v, want false", cfg.CheckerEnabled)
	}
}

func TestLoadSupportsMultipleTiDBDSNs(t *testing.T) {
	path := writeTempConfig(t, "dsn:\n  - mysql://node-a\n  - mysql://node-b\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := cfg.DSN
	want := []string{"mysql://node-a", "mysql://node-b"}
	if len(got) != len(want) {
		t.Fatalf("DSN len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DSN[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadRejectsLegacyScalarDSN(t *testing.T) {
	path := writeTempConfig(t, "dsn: mysql://legacy-single\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want legacy scalar dsn rejection")
	}
	if !strings.Contains(err.Error(), "cannot unmarshal") {
		t.Fatalf("Load() error = %q, want scalar type rejection", err)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	path := writeTempConfig(t, "unknown_key: true\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unknown key error")
	}
	if !strings.Contains(err.Error(), "field unknown_key not found") {
		t.Fatalf("Load() error = %q, want unknown key rejection", err)
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}
