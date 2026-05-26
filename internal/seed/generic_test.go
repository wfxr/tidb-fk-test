package seed

import (
	"reflect"
	"testing"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/config"
)

func TestGenericSeedPlanUsesHotParentCount(t *testing.T) {
	plan := GenericPlan(config.Config{
		SeedParentRowsPerTable: 1000,
		SeedHotParentKeys:      16,
	})

	if got := len(plan.HotParentIDs); got != 16 {
		t.Fatalf("len(HotParentIDs) = %d, want 16", got)
	}
}

func TestGenericSeedPlanUsesConfigDefaultsDeterministically(t *testing.T) {
	plan := GenericPlan(config.Default())

	if got := len(plan.ParentIDs); got != config.Default().SeedParentRowsPerTable {
		t.Fatalf("len(ParentIDs) = %d, want %d", got, config.Default().SeedParentRowsPerTable)
	}

	wantHotParents := []int64{1, 2, 3, 4, 5}
	if !reflect.DeepEqual(plan.HotParentIDs[:len(wantHotParents)], wantHotParents) {
		t.Fatalf("HotParentIDs prefix = %v, want %v", plan.HotParentIDs[:len(wantHotParents)], wantHotParents)
	}

	firstInsertSlot := plan.InsertExistingParentSlots[0]
	if firstInsertSlot.ChildID != 10001 || firstInsertSlot.ParentID != 1 {
		t.Fatalf("first InsertExistingParentSlots entry = %+v, want child 10001 parent 1", firstInsertSlot)
	}

	firstTxnSlot := plan.InsertParentThenChildSlots[0]
	if firstTxnSlot.ParentID != int64(config.Default().SeedParentRowsPerTable+1) || firstTxnSlot.ChildID != 20001 {
		t.Fatalf("first InsertParentThenChildSlots entry = %+v, want parent %d child 20001", firstTxnSlot, config.Default().SeedParentRowsPerTable+1)
	}
}

func TestGenericSeedPlanClampsHotParentCountToParentRows(t *testing.T) {
	plan := GenericPlan(config.Config{
		SeedParentRowsPerTable: 3,
		SeedHotParentKeys:      10,
	})

	want := []int64{1, 2, 3}
	if !reflect.DeepEqual(plan.HotParentIDs, want) {
		t.Fatalf("HotParentIDs = %v, want %v", plan.HotParentIDs, want)
	}
}

func TestGenericSeedPlanBuildsBoundedFixturePools(t *testing.T) {
	plan := GenericPlan(config.Config{
		SeedParentRowsPerTable: 5,
		SeedHotParentKeys:      2,
	})

	if got, want := len(plan.InsertExistingParentSlots), genericFixturePoolSize; got != want {
		t.Fatalf("len(InsertExistingParentSlots) = %d, want %d", got, want)
	}
	if got, want := len(plan.InsertParentThenChildSlots), genericFixturePoolSize; got != want {
		t.Fatalf("len(InsertParentThenChildSlots) = %d, want %d", got, want)
	}
	if got, want := len(plan.RebindChildSlots), genericFixturePoolSize; got != want {
		t.Fatalf("len(RebindChildSlots) = %d, want %d", got, want)
	}
	if got, want := len(plan.ConcurrentHotParentInsertSlots), genericFixturePoolSize; got != want {
		t.Fatalf("len(ConcurrentHotParentInsertSlots) = %d, want %d", got, want)
	}

	wantHotSlots := []HotChildSlot{
		{ChildID: 16001, ParentID: 1},
		{ChildID: 16002, ParentID: 2},
		{ChildID: 16003, ParentID: 1},
		{ChildID: 16004, ParentID: 2},
	}
	if !reflect.DeepEqual(plan.ConcurrentHotParentInsertSlots, wantHotSlots) {
		t.Fatalf("ConcurrentHotParentInsertSlots = %v, want %v", plan.ConcurrentHotParentInsertSlots, wantHotSlots)
	}
}

func TestGenericSeedPlanDoesNotFabricateMissingFixtures(t *testing.T) {
	plan := GenericPlan(config.Config{
		SeedParentRowsPerTable: 1,
		SeedHotParentKeys:      0,
	})

	if got, want := plan.ExistingParentID, int64(1); got != want {
		t.Fatalf("ExistingParentID = %d, want %d", got, want)
	}
	if plan.RebindParentID != 0 {
		t.Fatalf("RebindParentID = %d, want 0", plan.RebindParentID)
	}
	if got := len(plan.RebindChildSlots); got != 0 {
		t.Fatalf("len(RebindChildSlots) = %d, want 0", got)
	}
	if got := len(plan.ConcurrentHotParentInsertSlots); got != 0 {
		t.Fatalf("len(ConcurrentHotParentInsertSlots) = %d, want 0", got)
	}
}
