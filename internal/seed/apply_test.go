package seed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/config"
)

func TestApplyAllCreatesRealSchemaAndSeedsFixtureRows(t *testing.T) {
	db := newRecordingExec()

	cfg := config.Config{
		SeedParentRowsPerTable: 2,
		SeedHotParentKeys:      1,
	}
	applied, err := ApplyAll(context.Background(), db, cfg)
	if err != nil {
		t.Fatalf("ApplyAll() error = %v", err)
	}

	wantPhases := []string{
		phaseApplyMetadataSchema,
		phaseApplyGenericSchema,
		phaseApplyBillingSchema,
		phaseSeedGenericFixtures,
		phaseSeedBillingFixtures,
		phaseRecordPrepareMetadata,
	}
	if !reflect.DeepEqual(applied.CompletedPhases, wantPhases) {
		t.Fatalf("CompletedPhases = %v, want %v", applied.CompletedPhases, wantPhases)
	}

	wantCreated := []string{
		prepareMetadataTable,
		"parent_basic",
		"child_basic",
		"child_rebind",
		"parent_cascade",
		"child_cascade",
		"parent_hot",
		"child_hot",
		"probe_summary",
		"customer",
		"folio",
		"journal",
		"posting",
		"bill",
		"payment",
		"foliobalance",
		"feelog",
		"statement",
		"withdrawal",
	}
	for _, table := range wantCreated {
		if !db.createdTables[table] {
			t.Fatalf("table %q was not created; calls = %v", table, db.queries())
		}
	}

	wantGenericRows := map[string]int{
		prepareMetadataTable: 1,
		"parent_basic":       len(applied.Generic.ParentIDs),
		"child_basic":        len(applied.Generic.InsertExistingParentSlots) + len(applied.Generic.UpdateChildNoFKChangeSlots),
		"child_rebind":       len(applied.Generic.RebindChildSlots) + len(applied.Generic.InsertParentThenUpdateChildSlots),
		"parent_cascade":     len(applied.Generic.DeleteParentCascadeSlots),
		"child_cascade":      len(applied.Generic.DeleteParentCascadeSlots),
		"parent_hot":         len(applied.Generic.HotParentIDs),
		"child_hot":          len(applied.Generic.ConcurrentHotParentInsertSlots),
		"probe_summary":      1,
	}
	for table, want := range wantGenericRows {
		if got := db.insertedRows[table]; got != want {
			t.Fatalf("generic inserted rows for %s = %d, want %d", table, got, want)
		}
	}

	wantBillingRows := map[string]int{
		"customer":     len(applied.Billing.CustomerIDs),
		"folio":        len(applied.Billing.FolioIDs),
		"journal":      expectedJournalSeedRows(applied.Billing),
		"posting":      len(applied.Billing.JournalPostingBillSlots),
		"bill":         expectedBillSeedRows(applied.Billing),
		"payment":      len(applied.Billing.PaymentMixedReferenceSlots) + len(applied.Billing.PaymentBillUpdateProbeSlots),
		"foliobalance": len(applied.Billing.FolioBalanceUpdateSlots),
		"feelog":       len(applied.Billing.FKBackfillSlots),
		"statement":    len(applied.Billing.FKBackfillSlots) + len(applied.Billing.CascadePathSlots),
		"withdrawal":   len(applied.Billing.FKBackfillSlots) + len(applied.Billing.CascadePathSlots),
	}
	for table, want := range wantBillingRows {
		if got := db.insertedRows[table]; got != want {
			t.Fatalf("billing inserted rows for %s = %d, want %d", table, got, want)
		}
	}

	if got, want := applied.Generic.ExistingParentID, int64(1); got != want {
		t.Fatalf("Generic.ExistingParentID = %d, want %d", got, want)
	}
	if got, want := applied.Billing.ExistingBillID, int64(4001); got != want {
		t.Fatalf("Billing.ExistingBillID = %d, want %d", got, want)
	}
}

func TestApplyAllStopsAtFirstPhaseError(t *testing.T) {
	db := newRecordingExec()
	db.failOnQueryContaining = "CREATE TABLE IF NOT EXISTS customer"
	db.err = errors.New("billing schema failed")

	applied, err := ApplyAll(context.Background(), db, config.Config{
		SeedParentRowsPerTable: 2,
		SeedHotParentKeys:      1,
	})
	if err == nil {
		t.Fatal("ApplyAll() error = nil, want non-nil")
	}

	wantQueries := db.queries()
	if len(wantQueries) < 8 {
		t.Fatalf("expected multiple generic schema statements before failure, got %v", wantQueries)
	}

	wantPhases := []string{phaseApplyMetadataSchema, phaseApplyGenericSchema}
	if !reflect.DeepEqual(applied.CompletedPhases, wantPhases) {
		t.Fatalf("CompletedPhases = %v, want %v", applied.CompletedPhases, wantPhases)
	}

	if got := len(db.calls); got < 8 {
		t.Fatalf("len(calls) = %d, want at least 8", got)
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

var insertColumnsPerRow = map[string]int{
	prepareMetadataTable: 4,
	"parent_basic":       2,
	"child_basic":        3,
	"child_rebind":       3,
	"parent_cascade":     2,
	"child_cascade":      3,
	"parent_hot":         2,
	"child_hot":          3,
	"probe_summary":      3,
	"customer":           2,
	"folio":              3,
	"journal":            6,
	"posting":            4,
	"bill":               7,
	"payment":            5,
	"foliobalance":       4,
	"feelog":             4,
	"statement":          5,
	"withdrawal":         5,
}

type recordingExec struct {
	calls                 []execCall
	createdTables         map[string]bool
	insertedRows          map[string]int
	failOnQueryContaining string
	err                   error
}

func newRecordingExec() *recordingExec {
	return &recordingExec{
		createdTables: make(map[string]bool),
		insertedRows:  make(map[string]int),
	}
}

func (r *recordingExec) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.calls = append(r.calls, execCall{
		query: query,
		args:  append([]any(nil), args...),
	})

	if table, ok := createdTableName(query); ok {
		r.createdTables[table] = true
	}
	if table, ok := insertedTableName(query); ok {
		cols, found := insertColumnsPerRow[table]
		if !found {
			return stubResult{}, fmt.Errorf("unexpected insert table %q", table)
		}
		if len(args)%cols != 0 {
			return stubResult{}, fmt.Errorf("insert args for %s = %d, not divisible by %d", table, len(args), cols)
		}
		r.insertedRows[table] += len(args) / cols
	}

	if r.failOnQueryContaining != "" && strings.Contains(query, r.failOnQueryContaining) {
		return stubResult{}, r.err
	}

	return stubResult{}, nil
}

func (r *recordingExec) queries() []string {
	queries := make([]string, 0, len(r.calls))
	for _, call := range r.calls {
		queries = append(queries, call.query)
	}
	return queries
}

func createdTableName(query string) (string, bool) {
	const prefix = "CREATE TABLE IF NOT EXISTS "
	if !strings.HasPrefix(query, prefix) {
		return "", false
	}

	rest := strings.TrimPrefix(query, prefix)
	name, _, ok := strings.Cut(rest, " ")
	return name, ok
}

func insertedTableName(query string) (string, bool) {
	const prefix = "INSERT INTO "
	if !strings.HasPrefix(query, prefix) {
		return "", false
	}

	rest := strings.TrimPrefix(query, prefix)
	name, _, ok := strings.Cut(rest, " ")
	return name, ok
}

func expectedJournalSeedRows(plan BillingSeedPlan) int {
	if len(plan.CustomerIDs) == 0 {
		return 0
	}
	return 1 + len(plan.JournalPostingBillSlots) + len(plan.FKBackfillSlots) + len(plan.PaymentMixedReferenceSlots)
}

func expectedBillSeedRows(plan BillingSeedPlan) int {
	if len(plan.CustomerIDs) == 0 {
		return 0
	}
	return 1 + len(plan.JournalPostingBillSlots) + len(plan.FKBackfillSlots)
}
