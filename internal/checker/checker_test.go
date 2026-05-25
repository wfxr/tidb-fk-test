package checker

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/model"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/report"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/scenario"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/seed"
)

func TestCheckerIncludesOrphanChildQuery(t *testing.T) {
	checks := DefaultChecks()
	check, ok := checks["generic_orphan_child"]
	if !ok {
		t.Fatal("generic_orphan_child check missing")
	}
	if check.Kind != OrphanChildCheck {
		t.Fatalf("Kind = %q, want %q", check.Kind, OrphanChildCheck)
	}
}

func TestRunBuildsSummaryOrientedOutcomes(t *testing.T) {
	registry := scenario.NewRegistry()
	runtimeSummary := report.NewSummary(registry.All())

	probeScenario, ok := registry.Get("payment_bill_update_probe")
	if !ok {
		t.Fatal("payment_bill_update_probe not registered")
	}
	runtimeSummary.Record(probeScenario.Meta(), model.Result{
		Kind:      model.ExpectedFKFailure,
		ErrorText: "upgrading a shared lock to an exclusive lock is not supported",
	}, time.Date(2026, time.May, 25, 10, 0, 0, 0, time.UTC))

	db := &stubQueryer{
		rows: map[string]stubRow{
			defaultChecks["generic_orphan_child"].Query: {values: []any{int64(0)}},
			defaultChecks["cascade_result"].Query:       {values: []any{int64(0)}},
			defaultChecks["probe_error_match"].Query:    {values: []any{int64(1), int64(0)}},
		},
	}

	results, err := Run(context.Background(), db, seed.AppliedState{
		Generic: seed.GenericSeedPlan{
			DeleteParentCascadeSlots: []seed.CascadeSlot{{ParentID: 14001}},
		},
	}, runtimeSummary)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if got, want := results.Runtime.Totals.Executed, int64(1); got != want {
		t.Fatalf("Runtime.Executed = %d, want %d", got, want)
	}
	wantRuntimeScenarios := []report.ScenarioSummary{
		{
			Name:            "generic_concurrent_hot_parent_insert",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:            "generic_delete_parent_cascade",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:            "generic_insert_existing_parent",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:            "generic_insert_parent_then_child",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:            "generic_insert_parent_then_update_child_fk",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:            "generic_rebind_child_fk",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:            "generic_update_child_no_fk_change",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:              "payment_bill_update_probe",
			Executed:          1,
			Success:           0,
			ExpectedFailure:   1,
			UnexpectedFailure: 0,
			LastErrorAt:       time.Date(2026, time.May, 25, 10, 0, 0, 0, time.UTC),
			LastErrorText:     "upgrading a shared lock to an exclusive lock is not supported",
		},
		{
			Name:            "pm_cascade_path",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:            "pm_fk_backfill",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:            "pm_folio_balance_update",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:            "pm_journal_posting_bill",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
		{
			Name:            "pm_payment_mixed_references",
			Executed:        0,
			Success:         0,
			ExpectedFailure: 0,
		},
	}
	if !reflect.DeepEqual(results.Runtime.Scenarios, wantRuntimeScenarios) {
		t.Fatalf("Runtime.Scenarios = %#v, want %#v", results.Runtime.Scenarios, wantRuntimeScenarios)
	}

	wantOutcomes := []Outcome{
		{Name: "generic_orphan_child", Kind: OrphanChildCheck, Passed: true, Count: 0},
		{Name: "cascade_result", Kind: CascadeCheck, Passed: true, Count: 0},
		{Name: "probe_error_match", Kind: ProbeCheck, Passed: true, ExpectedFKFailure: 1, UnexpectedFailure: 0},
	}
	if !reflect.DeepEqual(results.Outcomes, wantOutcomes) {
		t.Fatalf("Outcomes = %#v, want %#v", results.Outcomes, wantOutcomes)
	}

	if got, want := db.calls[1].args, []any{int64(14001)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cascade args = %v, want %v", got, want)
	}
}

func TestRunReturnsCheckNameOnQueryFailure(t *testing.T) {
	db := &stubQueryer{
		rows: map[string]stubRow{
			defaultChecks["generic_orphan_child"].Query: {err: errors.New("scan failed")},
		},
	}

	_, err := Run(context.Background(), db, seed.AppliedState{}, nil)
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if got, want := err.Error(), "generic_orphan_child: scan failed"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

type stubQueryer struct {
	calls []queryCall
	rows  map[string]stubRow
}

type queryCall struct {
	query string
	args  []any
}

func (s *stubQueryer) QueryRowContext(_ context.Context, query string, args ...any) RowScanner {
	s.calls = append(s.calls, queryCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	row := s.rows[query]
	return row
}

type stubRow struct {
	values []any
	err    error
}

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch target := dest[i].(type) {
		case *int64:
			*target = r.values[i].(int64)
		default:
			return errors.New("unsupported scan target")
		}
	}
	return nil
}
