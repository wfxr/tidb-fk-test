package report

import (
	"errors"
	"testing"
	"time"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/model"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/scenario"
)

func TestReporterUsesConfiguredInterval(t *testing.T) {
	rep := NewProgressReporter(10 * time.Second)
	if rep.Interval() != 10*time.Second {
		t.Fatalf("Interval() = %v, want 10s", rep.Interval())
	}
}

func TestSummaryAggregatesScenarioOutcomesAndLastError(t *testing.T) {
	registry := scenario.NewRegistry()
	summary := NewSummary(registry.All())

	genericScenario, ok := registry.Get("generic_insert_existing_parent")
	if !ok {
		t.Fatal("generic_insert_existing_parent not registered")
	}

	summary.Record(genericScenario.Meta(), model.Result{Kind: model.Success}, time.Time{})

	expectedErr := errors.New("upgrading a shared lock to an exclusive lock is not supported")
	probeScenario, ok := registry.Get("payment_bill_update_probe")
	if !ok {
		t.Fatal("payment_bill_update_probe not registered")
	}
	expectedAt := time.Date(2026, time.May, 25, 10, 0, 0, 0, time.UTC)
	summary.Record(probeScenario.Meta(), model.Result{
		Kind:      model.ExpectedFKFailure,
		ErrorText: expectedErr.Error(),
	}, expectedAt)

	unexpectedAt := expectedAt.Add(30 * time.Second)
	summary.Record(genericScenario.Meta(), model.Result{
		Kind:      model.InfraOrUpgradeTransient,
		ErrorText: "driver: bad connection",
	}, unexpectedAt)

	genericStats, ok := summary.Scenario("generic_insert_existing_parent")
	if !ok {
		t.Fatal("Scenario(generic_insert_existing_parent) not found")
	}
	if genericStats.Executed != 2 {
		t.Fatalf("Executed = %d, want 2", genericStats.Executed)
	}
	if genericStats.Success != 1 {
		t.Fatalf("Success = %d, want 1", genericStats.Success)
	}
	if genericStats.ExpectedFailure != 0 {
		t.Fatalf("ExpectedFailure = %d, want 0", genericStats.ExpectedFailure)
	}
	if genericStats.UnexpectedFailure != 1 {
		t.Fatalf("UnexpectedFailure = %d, want 1", genericStats.UnexpectedFailure)
	}
	if genericStats.LastErrorAt != unexpectedAt {
		t.Fatalf("LastErrorAt = %v, want %v", genericStats.LastErrorAt, unexpectedAt)
	}
	if genericStats.LastErrorText != "driver: bad connection" {
		t.Fatalf("LastErrorText = %q, want %q", genericStats.LastErrorText, "driver: bad connection")
	}

	probeStats, ok := summary.Scenario("payment_bill_update_probe")
	if !ok {
		t.Fatal("Scenario(payment_bill_update_probe) not found")
	}
	if probeStats.Executed != 1 {
		t.Fatalf("Executed = %d, want 1", probeStats.Executed)
	}
	if probeStats.ExpectedFailure != 1 {
		t.Fatalf("ExpectedFailure = %d, want 1", probeStats.ExpectedFailure)
	}
	if probeStats.LastErrorAt != expectedAt {
		t.Fatalf("LastErrorAt = %v, want %v", probeStats.LastErrorAt, expectedAt)
	}
	if probeStats.LastErrorText != expectedErr.Error() {
		t.Fatalf("LastErrorText = %q, want %q", probeStats.LastErrorText, expectedErr.Error())
	}

	totals := summary.Totals()
	if totals.Executed != 3 {
		t.Fatalf("Totals().Executed = %d, want 3", totals.Executed)
	}
	if totals.Success != 1 {
		t.Fatalf("Totals().Success = %d, want 1", totals.Success)
	}
	if totals.ExpectedFailure != 1 {
		t.Fatalf("Totals().ExpectedFailure = %d, want 1", totals.ExpectedFailure)
	}
	if totals.UnexpectedFailure != 1 {
		t.Fatalf("Totals().UnexpectedFailure = %d, want 1", totals.UnexpectedFailure)
	}
}

func TestProgressReporterBuildsSnapshotFromSummary(t *testing.T) {
	registry := scenario.NewRegistry()
	summary := NewSummary(registry.All())

	genericScenario, ok := registry.Get("generic_insert_existing_parent")
	if !ok {
		t.Fatal("generic_insert_existing_parent not registered")
	}
	probeScenario, ok := registry.Get("payment_bill_update_probe")
	if !ok {
		t.Fatal("payment_bill_update_probe not registered")
	}

	summary.Record(genericScenario.Meta(), model.Result{Kind: model.Success}, time.Time{})
	summary.Record(probeScenario.Meta(), model.Result{
		Kind:      model.ExpectedFKFailure,
		ErrorText: "upgrading a shared lock to an exclusive lock is not supported",
	}, time.Date(2026, time.May, 25, 10, 0, 0, 0, time.UTC))
	summary.Record(genericScenario.Meta(), model.Result{
		Kind:      model.UnexpectedFailure,
		ErrorText: "unexpected foreign key failure",
	}, time.Date(2026, time.May, 25, 10, 0, 30, 0, time.UTC))

	reporter := NewProgressReporter(10 * time.Second)
	now := time.Date(2026, time.May, 25, 10, 1, 0, 0, time.UTC)
	snapshot := reporter.BuildSnapshot("during-upgrade", 3, summary, now)

	if snapshot.Timestamp != now {
		t.Fatalf("Timestamp = %v, want %v", snapshot.Timestamp, now)
	}
	if snapshot.Phase != "during-upgrade" {
		t.Fatalf("Phase = %q, want during-upgrade", snapshot.Phase)
	}
	if snapshot.ActiveWorkers != 3 {
		t.Fatalf("ActiveWorkers = %d, want 3", snapshot.ActiveWorkers)
	}
	if snapshot.TotalExecuted != 3 {
		t.Fatalf("TotalExecuted = %d, want 3", snapshot.TotalExecuted)
	}
	if snapshot.Success != 1 {
		t.Fatalf("Success = %d, want 1", snapshot.Success)
	}
	if snapshot.ExpectedFailure != 1 {
		t.Fatalf("ExpectedFailure = %d, want 1", snapshot.ExpectedFailure)
	}
	if snapshot.UnexpectedFailure != 1 {
		t.Fatalf("UnexpectedFailure = %d, want 1", snapshot.UnexpectedFailure)
	}
}
