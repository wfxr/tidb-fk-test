package seed

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"
)

func TestApplyAllRunsPhasesInOrder(t *testing.T) {
	db := &stubExec{}

	applied, err := ApplyAll(context.Background(), db, config.Config{
		SeedParentRowsPerTable: 2,
		SeedHotParentKeys:      1,
	})
	if err != nil {
		t.Fatalf("ApplyAll() error = %v", err)
	}

	wantPhases := []string{
		phaseApplyGenericSchema,
		phaseApplyPropertyMeSchema,
		phaseSeedGenericFixtures,
		phaseSeedPropertyMeFixtures,
	}
	if !reflect.DeepEqual(applied.CompletedPhases, wantPhases) {
		t.Fatalf("CompletedPhases = %v, want %v", applied.CompletedPhases, wantPhases)
	}

	var gotQueries []string
	for _, call := range db.calls {
		gotQueries = append(gotQueries, call.query)
	}
	wantQueries := []string{
		applyGenericSchemaStatement,
		applyPropertyMeSchemaStatement,
		seedGenericFixturesStatement,
		seedPropertyMeFixturesStatement,
	}
	if !reflect.DeepEqual(gotQueries, wantQueries) {
		t.Fatalf("queries = %v, want %v", gotQueries, wantQueries)
	}

	if got, want := applied.Generic.ExistingParentID, int64(1); got != want {
		t.Fatalf("Generic.ExistingParentID = %d, want %d", got, want)
	}
	if got, want := applied.PropertyMe.ExistingBillID, int64(4001); got != want {
		t.Fatalf("PropertyMe.ExistingBillID = %d, want %d", got, want)
	}
}

func TestApplyAllStopsAtFirstPhaseError(t *testing.T) {
	db := &stubExec{
		failAt: 2,
		err:    errors.New("propertyme schema failed"),
	}

	applied, err := ApplyAll(context.Background(), db, config.Config{
		SeedParentRowsPerTable: 2,
		SeedHotParentKeys:      1,
	})
	if err == nil {
		t.Fatal("ApplyAll() error = nil, want non-nil")
	}

	wantPhases := []string{phaseApplyGenericSchema}
	if !reflect.DeepEqual(applied.CompletedPhases, wantPhases) {
		t.Fatalf("CompletedPhases = %v, want %v", applied.CompletedPhases, wantPhases)
	}

	if got := len(db.calls); got != 2 {
		t.Fatalf("len(calls) = %d, want 2", got)
	}
}

type execCall struct {
	query string
	args  []any
}

type stubExec struct {
	calls  []execCall
	failAt int
	err    error
}

func (s *stubExec) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.calls = append(s.calls, execCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	if s.failAt > 0 && len(s.calls) == s.failAt {
		return stubResult{}, s.err
	}
	return stubResult{}, nil
}

type stubResult struct{}

func (stubResult) LastInsertId() (int64, error) { return 0, nil }

func (stubResult) RowsAffected() (int64, error) { return 0, nil }
