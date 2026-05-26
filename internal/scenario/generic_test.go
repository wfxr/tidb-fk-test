package scenario

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/config"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/db"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/seed"
)

var _ Scenario = NewGenericInsertExistingParent()
var _ Scenario = NewGenericInsertParentThenChild()
var _ Scenario = NewGenericUpdateChildNoFKChange()
var _ Scenario = NewGenericRebindChildFK()
var _ Scenario = NewGenericInsertParentThenUpdateChildFK()
var _ Scenario = NewGenericDeleteParentCascade()
var _ Scenario = NewGenericConcurrentHotParentInsert()

type execCall struct {
	query string
	args  []any
}

type recordingTxSession struct {
	calls  []execCall
	failAt int
	err    error
}

func (s *recordingTxSession) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	callIndex := len(s.calls) + 1
	s.calls = append(s.calls, execCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	if s.failAt == callIndex {
		if s.err == nil {
			s.err = errors.New("forced exec failure")
		}
		return nil, s.err
	}
	return nil, nil
}

func (s *recordingTxSession) QueryRowContext(context.Context, string, ...any) db.RowScanner {
	return nil
}

func TestGenericScenarioMetadata(t *testing.T) {
	tests := []struct {
		name string
		got  Scenario
	}{
		{name: "generic_insert_existing_parent", got: NewGenericInsertExistingParent()},
		{name: "generic_insert_parent_then_child", got: NewGenericInsertParentThenChild()},
		{name: "generic_update_child_no_fk_change", got: NewGenericUpdateChildNoFKChange()},
		{name: "generic_rebind_child_fk", got: NewGenericRebindChildFK()},
		{name: "generic_insert_parent_then_update_child_fk", got: NewGenericInsertParentThenUpdateChildFK()},
		{name: "generic_delete_parent_cascade", got: NewGenericDeleteParentCascade()},
		{name: "generic_concurrent_hot_parent_insert", got: NewGenericConcurrentHotParentInsert()},
	}

	for _, tt := range tests {
		meta := tt.got.Meta()
		if meta.Name != tt.name {
			t.Fatalf("%T Meta().Name = %q, want %q", tt.got, meta.Name, tt.name)
		}
		if meta.Group != GenericGroup {
			t.Fatalf("%s Meta().Group = %q, want %q", tt.name, meta.Group, GenericGroup)
		}
	}
}

func TestGenericInsertExistingParentUsesInsertShape(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	slot := seedState.Generic.InsertExistingParentSlots[0]

	err := NewGenericInsertExistingParent().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM child_basic WHERE id = ?",
		"INSERT INTO child_basic (id, parent_id, payload) VALUES (?, ?, ?)",
	}
	if got := queriesFromCalls(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.ChildID},
		{slot.ChildID, slot.ParentID, "child-insert"},
	}
	if got := argsFromCalls(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestGenericInsertExistingParentCyclesFixtureSlots(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	scenario := NewGenericInsertExistingParent()

	if err := scenario.Run(context.Background(), sess, seedState); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if err := scenario.Run(context.Background(), sess, seedState); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	gotArgs := argsFromCalls(sess.calls)
	wantArgs := [][]any{
		{seedState.Generic.InsertExistingParentSlots[0].ChildID},
		{seedState.Generic.InsertExistingParentSlots[0].ChildID, seedState.Generic.InsertExistingParentSlots[0].ParentID, "child-insert"},
		{seedState.Generic.InsertExistingParentSlots[1].ChildID},
		{seedState.Generic.InsertExistingParentSlots[1].ChildID, seedState.Generic.InsertExistingParentSlots[1].ParentID, "child-insert"},
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestGenericInsertParentThenChildOrder(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	slot := seedState.Generic.InsertParentThenChildSlots[0]

	err := NewGenericInsertParentThenChild().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM child_basic WHERE id = ?",
		"DELETE FROM parent_basic WHERE id = ?",
		"INSERT INTO parent_basic (id, note) VALUES (?, ?)",
		"INSERT INTO child_basic (id, parent_id, payload) VALUES (?, ?, ?)",
	}
	if got := queriesFromCalls(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.ChildID},
		{slot.ParentID},
		{slot.ParentID, "parent-insert"},
		{slot.ChildID, slot.ParentID, "child-insert"},
	}
	if got := argsFromCalls(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestGenericInsertParentThenChildCyclesFixtureSlots(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	scenario := NewGenericInsertParentThenChild()

	if err := scenario.Run(context.Background(), sess, seedState); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if err := scenario.Run(context.Background(), sess, seedState); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	wantArgs := [][]any{
		{seedState.Generic.InsertParentThenChildSlots[0].ChildID},
		{seedState.Generic.InsertParentThenChildSlots[0].ParentID},
		{seedState.Generic.InsertParentThenChildSlots[0].ParentID, "parent-insert"},
		{seedState.Generic.InsertParentThenChildSlots[0].ChildID, seedState.Generic.InsertParentThenChildSlots[0].ParentID, "child-insert"},
		{seedState.Generic.InsertParentThenChildSlots[1].ChildID},
		{seedState.Generic.InsertParentThenChildSlots[1].ParentID},
		{seedState.Generic.InsertParentThenChildSlots[1].ParentID, "parent-insert"},
		{seedState.Generic.InsertParentThenChildSlots[1].ChildID, seedState.Generic.InsertParentThenChildSlots[1].ParentID, "child-insert"},
	}
	if got := argsFromCalls(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestGenericInsertParentThenChildStopsAfterFirstError(t *testing.T) {
	wantErr := errors.New("delete child failed")
	sess := &recordingTxSession{
		failAt: 1,
		err:    wantErr,
	}

	err := NewGenericInsertParentThenChild().Run(context.Background(), sess, testGenericSeedState())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}

	if got := len(sess.calls); got != 1 {
		t.Fatalf("len(calls) = %d, want 1", got)
	}
}

func TestGenericUpdateChildNoFKChangeOrder(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	slot := seedState.Generic.UpdateChildNoFKChangeSlots[0]

	err := NewGenericUpdateChildNoFKChange().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM child_basic WHERE id = ?",
		"INSERT INTO child_basic (id, parent_id, payload) VALUES (?, ?, ?)",
		"UPDATE child_basic SET payload = ? WHERE id = ?",
	}
	if got := queriesFromCalls(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.ChildID},
		{slot.ChildID, slot.ParentID, "child-baseline"},
		{"child-updated", slot.ChildID},
	}
	if got := argsFromCalls(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestGenericRebindChildFKUsesRuntimeSeedAndSchema(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	slot := seedState.Generic.RebindChildSlots[0]

	err := NewGenericRebindChildFK().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM child_rebind WHERE id = ?",
		"INSERT INTO child_rebind (id, parent_id, payload) VALUES (?, ?, ?)",
		"UPDATE child_rebind SET parent_id = ? WHERE id = ?",
	}
	if got := queriesFromCalls(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.ChildID},
		{slot.ChildID, slot.BaseParentID, "child-baseline"},
		{slot.TargetParentID, slot.ChildID},
	}
	if got := argsFromCalls(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestGenericInsertParentThenUpdateChildFKResetsBeforeRebinding(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	slot := seedState.Generic.InsertParentThenUpdateChildSlots[0]

	err := NewGenericInsertParentThenUpdateChildFK().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM child_rebind WHERE id = ?",
		"INSERT INTO child_rebind (id, parent_id, payload) VALUES (?, ?, ?)",
		"DELETE FROM parent_basic WHERE id = ?",
		"INSERT INTO parent_basic (id, note) VALUES (?, ?)",
		"UPDATE child_rebind SET parent_id = ? WHERE id = ?",
	}
	if got := queriesFromCalls(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.ChildID},
		{slot.ChildID, slot.BaseParentID, "child-baseline"},
		{slot.TargetParentID},
		{slot.TargetParentID, "parent-insert"},
		{slot.TargetParentID, slot.ChildID},
	}
	if got := argsFromCalls(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestGenericInsertParentThenUpdateChildFKCyclesFixtureSlots(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	scenario := NewGenericInsertParentThenUpdateChildFK()

	if err := scenario.Run(context.Background(), sess, seedState); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if err := scenario.Run(context.Background(), sess, seedState); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	gotArgs := argsFromCalls(sess.calls)
	first := seedState.Generic.InsertParentThenUpdateChildSlots[0]
	second := seedState.Generic.InsertParentThenUpdateChildSlots[1]
	wantArgs := [][]any{
		{first.ChildID},
		{first.ChildID, first.BaseParentID, "child-baseline"},
		{first.TargetParentID},
		{first.TargetParentID, "parent-insert"},
		{first.TargetParentID, first.ChildID},
		{second.ChildID},
		{second.ChildID, second.BaseParentID, "child-baseline"},
		{second.TargetParentID},
		{second.TargetParentID, "parent-insert"},
		{second.TargetParentID, second.ChildID},
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestGenericDeleteParentCascadeExecutesDelete(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	slot := seedState.Generic.DeleteParentCascadeSlots[0]

	err := NewGenericDeleteParentCascade().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM child_cascade WHERE id = ?",
		"DELETE FROM parent_cascade WHERE id = ?",
		"INSERT INTO parent_cascade (id, note) VALUES (?, ?)",
		"INSERT INTO child_cascade (id, parent_id, payload) VALUES (?, ?, ?)",
		"DELETE FROM parent_cascade WHERE id = ?",
	}
	if got := queriesFromCalls(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.ChildID},
		{slot.ParentID},
		{slot.ParentID, "cascade-parent"},
		{slot.ChildID, slot.ParentID, "cascade-child"},
		{slot.ParentID},
	}
	if got := argsFromCalls(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestGenericConcurrentHotParentInsertUsesHotParentID(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	slot := seedState.Generic.ConcurrentHotParentInsertSlots[0]

	err := NewGenericConcurrentHotParentInsert().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM child_hot WHERE id = ?",
		"INSERT INTO child_hot (id, parent_id, payload) VALUES (?, ?, ?)",
	}
	if got := queriesFromCalls(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.ChildID},
		{slot.ChildID, slot.ParentID, "hot-child"},
	}
	if got := argsFromCalls(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestGenericConcurrentHotParentInsertCyclesFixtureSlots(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testGenericSeedState()
	scenario := NewGenericConcurrentHotParentInsert()

	if err := scenario.Run(context.Background(), sess, seedState); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if err := scenario.Run(context.Background(), sess, seedState); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	gotArgs := argsFromCalls(sess.calls)
	first := seedState.Generic.ConcurrentHotParentInsertSlots[0]
	second := seedState.Generic.ConcurrentHotParentInsertSlots[1]
	wantArgs := [][]any{
		{first.ChildID},
		{first.ChildID, first.ParentID, "hot-child"},
		{second.ChildID},
		{second.ChildID, second.ParentID, "hot-child"},
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
}

func TestGenericScenarioRequiresRuntimeSeedPlan(t *testing.T) {
	sess := &recordingTxSession{}

	err := NewGenericInsertExistingParent().Run(context.Background(), sess, SeedState{})
	if err == nil {
		t.Fatal("Run() error = nil, want missing generic seed plan error")
	}
	if got := len(sess.calls); got != 0 {
		t.Fatalf("len(calls) = %d, want 0", got)
	}
}

func TestGenericRebindChildFKFailsWhenPlanLacksTwoParents(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := SeedState{
		Generic: seed.GenericPlan(config.Config{
			SeedParentRowsPerTable: 1,
			SeedHotParentKeys:      1,
		}),
	}

	err := NewGenericRebindChildFK().Run(context.Background(), sess, seedState)
	if err == nil {
		t.Fatal("Run() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "requires at least 2 seeded parent rows") {
		t.Fatalf("Run() error = %v, want two-parent validation", err)
	}
	if got := len(sess.calls); got != 0 {
		t.Fatalf("len(calls) = %d, want 0", got)
	}
}

func TestGenericConcurrentHotParentInsertFailsWhenPlanLacksHotParents(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := SeedState{
		Generic: seed.GenericPlan(config.Config{
			SeedParentRowsPerTable: 2,
			SeedHotParentKeys:      0,
		}),
	}

	err := NewGenericConcurrentHotParentInsert().Run(context.Background(), sess, seedState)
	if err == nil {
		t.Fatal("Run() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "requires at least 1 hot parent key") {
		t.Fatalf("Run() error = %v, want hot-parent validation", err)
	}
	if got := len(sess.calls); got != 0 {
		t.Fatalf("len(calls) = %d, want 0", got)
	}
}

func queriesFromCalls(calls []execCall) []string {
	queries := make([]string, 0, len(calls))
	for _, call := range calls {
		queries = append(queries, call.query)
	}
	return queries
}

func argsFromCalls(calls []execCall) [][]any {
	args := make([][]any, 0, len(calls))
	for _, call := range calls {
		args = append(args, call.args)
	}
	return args
}

func testGenericSeedState() SeedState {
	return SeedState{
		Generic: seed.GenericSeedPlan{
			ParentIDs:        []int64{101, 102, 103},
			HotParentIDs:     []int64{701, 702},
			ExistingParentID: 101,
			RebindParentID:   102,
			InsertExistingParentSlots: []seed.BasicChildSlot{
				{ChildID: 201, ParentID: 101},
				{ChildID: 202, ParentID: 101},
			},
			UpdateChildNoFKChangeSlots: []seed.BasicChildSlot{
				{ChildID: 301, ParentID: 101},
				{ChildID: 302, ParentID: 101},
			},
			RebindChildSlots: []seed.RebindSlot{
				{ChildID: 401, BaseParentID: 101, TargetParentID: 102},
				{ChildID: 402, BaseParentID: 101, TargetParentID: 102},
			},
			InsertParentThenChildSlots: []seed.ParentChildSlot{
				{ParentID: 501, ChildID: 601},
				{ParentID: 502, ChildID: 602},
			},
			InsertParentThenUpdateChildSlots: []seed.RebindSlot{
				{ChildID: 701, BaseParentID: 101, TargetParentID: 801},
				{ChildID: 702, BaseParentID: 101, TargetParentID: 802},
			},
			DeleteParentCascadeSlots: []seed.CascadeSlot{
				{ParentID: 901, ChildID: 902},
				{ParentID: 903, ChildID: 904},
			},
			ConcurrentHotParentInsertSlots: []seed.HotChildSlot{
				{ChildID: 1001, ParentID: 701},
				{ChildID: 1002, ParentID: 702},
			},
		},
	}
}
