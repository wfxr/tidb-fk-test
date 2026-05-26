package scenario

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/db"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/seed"
)

func NewGenericInsertExistingParent() Scenario {
	return newGenericExecScenario(
		"generic_insert_existing_parent",
		1,
		func(plan seed.GenericSeedPlan, run uint64) ([]execStep, error) {
			slot, err := selectPoolSlot(
				"generic_insert_existing_parent",
				plan.InsertExistingParentSlots,
				run,
				"at least 1 seeded parent row",
			)
			if err != nil {
				return nil, err
			}

			return []execStep{
				{query: "DELETE FROM child_basic WHERE id = ?", args: []any{slot.ChildID}},
				{
					query: "INSERT INTO child_basic (id, parent_id, payload) VALUES (?, ?, ?)",
					args:  []any{slot.ChildID, slot.ParentID, "child-insert"},
				},
			}, nil
		},
	)
}

func NewGenericInsertParentThenChild() Scenario {
	return newGenericExecScenario(
		"generic_insert_parent_then_child",
		1,
		func(plan seed.GenericSeedPlan, run uint64) ([]execStep, error) {
			slot, err := selectPoolSlot(
				"generic_insert_parent_then_child",
				plan.InsertParentThenChildSlots,
				run,
				"parent-child fixture slots",
			)
			if err != nil {
				return nil, err
			}

			return []execStep{
				{query: "DELETE FROM child_basic WHERE id = ?", args: []any{slot.ChildID}},
				{query: "DELETE FROM parent_basic WHERE id = ?", args: []any{slot.ParentID}},
				{
					query: "INSERT INTO parent_basic (id, note) VALUES (?, ?)",
					args:  []any{slot.ParentID, "parent-insert"},
				},
				{
					query: "INSERT INTO child_basic (id, parent_id, payload) VALUES (?, ?, ?)",
					args:  []any{slot.ChildID, slot.ParentID, "child-insert"},
				},
			}, nil
		},
	)
}

func NewGenericUpdateChildNoFKChange() Scenario {
	return newGenericExecScenario(
		"generic_update_child_no_fk_change",
		1,
		func(plan seed.GenericSeedPlan, run uint64) ([]execStep, error) {
			slot, err := selectPoolSlot(
				"generic_update_child_no_fk_change",
				plan.UpdateChildNoFKChangeSlots,
				run,
				"at least 1 seeded parent row",
			)
			if err != nil {
				return nil, err
			}

			return []execStep{
				{query: "DELETE FROM child_basic WHERE id = ?", args: []any{slot.ChildID}},
				{
					query: "INSERT INTO child_basic (id, parent_id, payload) VALUES (?, ?, ?)",
					args:  []any{slot.ChildID, slot.ParentID, "child-baseline"},
				},
				{
					query: "UPDATE child_basic SET payload = ? WHERE id = ?",
					args:  []any{"child-updated", slot.ChildID},
				},
			}, nil
		},
	)
}

func NewGenericRebindChildFK() Scenario {
	return newGenericExecScenario(
		"generic_rebind_child_fk",
		1,
		func(plan seed.GenericSeedPlan, run uint64) ([]execStep, error) {
			slot, err := selectPoolSlot(
				"generic_rebind_child_fk",
				plan.RebindChildSlots,
				run,
				"at least 2 seeded parent rows",
			)
			if err != nil {
				return nil, err
			}

			return []execStep{
				{query: "DELETE FROM child_rebind WHERE id = ?", args: []any{slot.ChildID}},
				{
					query: "INSERT INTO child_rebind (id, parent_id, payload) VALUES (?, ?, ?)",
					args:  []any{slot.ChildID, slot.BaseParentID, "child-baseline"},
				},
				{
					query: "UPDATE child_rebind SET parent_id = ? WHERE id = ?",
					args:  []any{slot.TargetParentID, slot.ChildID},
				},
			}, nil
		},
	)
}

func NewGenericInsertParentThenUpdateChildFK() Scenario {
	return newGenericExecScenario(
		"generic_insert_parent_then_update_child_fk",
		1,
		func(plan seed.GenericSeedPlan, run uint64) ([]execStep, error) {
			slot, err := selectPoolSlot(
				"generic_insert_parent_then_update_child_fk",
				plan.InsertParentThenUpdateChildSlots,
				run,
				"at least 1 seeded parent row",
			)
			if err != nil {
				return nil, err
			}

			return []execStep{
				{query: "DELETE FROM child_rebind WHERE id = ?", args: []any{slot.ChildID}},
				{
					query: "INSERT INTO child_rebind (id, parent_id, payload) VALUES (?, ?, ?)",
					args:  []any{slot.ChildID, slot.BaseParentID, "child-baseline"},
				},
				{query: "DELETE FROM parent_basic WHERE id = ?", args: []any{slot.TargetParentID}},
				{
					query: "INSERT INTO parent_basic (id, note) VALUES (?, ?)",
					args:  []any{slot.TargetParentID, "parent-insert"},
				},
				{
					query: "UPDATE child_rebind SET parent_id = ? WHERE id = ?",
					args:  []any{slot.TargetParentID, slot.ChildID},
				},
			}, nil
		},
	)
}

func NewGenericDeleteParentCascade() Scenario {
	return newGenericExecScenario(
		"generic_delete_parent_cascade",
		1,
		func(plan seed.GenericSeedPlan, run uint64) ([]execStep, error) {
			slot, err := selectPoolSlot(
				"generic_delete_parent_cascade",
				plan.DeleteParentCascadeSlots,
				run,
				"cascade fixture slots",
			)
			if err != nil {
				return nil, err
			}

			return []execStep{
				{query: "DELETE FROM child_cascade WHERE id = ?", args: []any{slot.ChildID}},
				{query: "DELETE FROM parent_cascade WHERE id = ?", args: []any{slot.ParentID}},
				{
					query: "INSERT INTO parent_cascade (id, note) VALUES (?, ?)",
					args:  []any{slot.ParentID, "cascade-parent"},
				},
				{
					query: "INSERT INTO child_cascade (id, parent_id, payload) VALUES (?, ?, ?)",
					args:  []any{slot.ChildID, slot.ParentID, "cascade-child"},
				},
				{
					query: "DELETE FROM parent_cascade WHERE id = ?",
					args:  []any{slot.ParentID},
				},
			}, nil
		},
	)
}

func NewGenericConcurrentHotParentInsert() Scenario {
	return newGenericExecScenario(
		"generic_concurrent_hot_parent_insert",
		2,
		func(plan seed.GenericSeedPlan, run uint64) ([]execStep, error) {
			slot, err := selectPoolSlot(
				"generic_concurrent_hot_parent_insert",
				plan.ConcurrentHotParentInsertSlots,
				run,
				"at least 1 hot parent key",
			)
			if err != nil {
				return nil, err
			}

			return []execStep{
				{query: "DELETE FROM child_hot WHERE id = ?", args: []any{slot.ChildID}},
				{
					query: "INSERT INTO child_hot (id, parent_id, payload) VALUES (?, ?, ?)",
					args:  []any{slot.ChildID, slot.ParentID, "hot-child"},
				},
			}, nil
		},
	)
}

type execStep struct {
	query string
	args  []any
}

type genericExecScenario struct {
	meta       Metadata
	runCounter atomic.Uint64
	buildSteps func(plan seed.GenericSeedPlan, run uint64) ([]execStep, error)
}

func newGenericExecScenario(
	name string,
	concurrencyHint int,
	buildSteps func(plan seed.GenericSeedPlan, run uint64) ([]execStep, error),
) Scenario {
	return &genericExecScenario{
		meta: Metadata{
			Name:            name,
			Group:           GenericGroup,
			Weight:          1,
			ConcurrencyHint: concurrencyHint,
		},
		buildSteps: buildSteps,
	}
}

func (s *genericExecScenario) Meta() Metadata {
	return s.meta
}

func (s *genericExecScenario) Run(ctx context.Context, sess db.TxSession, seedState SeedState) error {
	if missingGenericSeedPlan(seedState.Generic) {
		return fmt.Errorf("%s: generic seed plan required", s.meta.Name)
	}

	run := s.runCounter.Add(1) - 1
	steps, err := s.buildSteps(seedState.Generic, run)
	if err != nil {
		return err
	}

	for _, step := range cloneExecSteps(steps) {
		if _, err := sess.ExecContext(ctx, step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}

func cloneExecSteps(steps []execStep) []execStep {
	cloned := make([]execStep, 0, len(steps))
	for _, step := range steps {
		cloned = append(cloned, execStep{
			query: step.query,
			args:  append([]any(nil), step.args...),
		})
	}
	return cloned
}

func missingGenericSeedPlan(plan seed.GenericSeedPlan) bool {
	return len(plan.ParentIDs) == 0 &&
		len(plan.HotParentIDs) == 0 &&
		len(plan.InsertExistingParentSlots) == 0 &&
		len(plan.UpdateChildNoFKChangeSlots) == 0 &&
		len(plan.RebindChildSlots) == 0 &&
		len(plan.InsertParentThenChildSlots) == 0 &&
		len(plan.InsertParentThenUpdateChildSlots) == 0 &&
		len(plan.DeleteParentCascadeSlots) == 0 &&
		len(plan.ConcurrentHotParentInsertSlots) == 0
}

func selectPoolSlot[T any](scenarioName string, slots []T, run uint64, requirement string) (T, error) {
	var zero T
	if len(slots) == 0 {
		return zero, fmt.Errorf("%s requires %s", scenarioName, requirement)
	}
	return slots[run%uint64(len(slots))], nil
}
