package checker

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/seed"
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
	db := &stubQueryer{
		rows: map[string]stubRow{
			defaultChecks["generic_orphan_child"].Query: {values: []any{int64(0)}},
			defaultChecks["cascade_result"].Query:       {values: []any{int64(0)}},
		},
	}

	results, err := Run(context.Background(), db, seed.AppliedState{
		Generic: seed.GenericSeedPlan{
			DeleteParentCascadeSlots: []seed.CascadeSlot{{ParentID: 14001}},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantOutcomes := []Outcome{
		{Name: "generic_orphan_child", Kind: OrphanChildCheck, Passed: true, Count: 0},
		{Name: "cascade_result", Kind: CascadeCheck, Passed: true, Count: 0},
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
