package seed

import "github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"

const genericFixturePoolSize = 4

type BasicChildSlot struct {
	ChildID  int64
	ParentID int64
}

type ParentChildSlot struct {
	ParentID int64
	ChildID  int64
}

type RebindSlot struct {
	ChildID        int64
	BaseParentID   int64
	TargetParentID int64
}

type CascadeSlot struct {
	ParentID int64
	ChildID  int64
}

type HotChildSlot struct {
	ParentID int64
	ChildID  int64
}

type GenericSeedPlan struct {
	ParentIDs                        []int64
	HotParentIDs                     []int64
	ExistingParentID                 int64
	RebindParentID                   int64
	InsertExistingParentSlots        []BasicChildSlot
	UpdateChildNoFKChangeSlots       []BasicChildSlot
	RebindChildSlots                 []RebindSlot
	InsertParentThenChildSlots       []ParentChildSlot
	InsertParentThenUpdateChildSlots []RebindSlot
	DeleteParentCascadeSlots         []CascadeSlot
	ConcurrentHotParentInsertSlots   []HotChildSlot
}

func GenericPlan(cfg config.Config) GenericSeedPlan {
	parentCount := max(cfg.SeedParentRowsPerTable, 0)
	parentIDs := make([]int64, parentCount)
	for i := range parentIDs {
		parentIDs[i] = int64(i + 1)
	}

	hotCount := cfg.SeedHotParentKeys
	if hotCount < 0 {
		hotCount = 0
	}
	if hotCount > len(parentIDs) {
		hotCount = len(parentIDs)
	}

	hotParentIDs := append([]int64(nil), parentIDs[:hotCount]...)

	plan := GenericSeedPlan{
		ParentIDs:    parentIDs,
		HotParentIDs: hotParentIDs,
	}

	if len(parentIDs) >= 1 {
		plan.ExistingParentID = parentIDs[0]
		plan.InsertExistingParentSlots = buildBasicChildSlots(10001, plan.ExistingParentID)
		plan.UpdateChildNoFKChangeSlots = buildBasicChildSlots(11001, plan.ExistingParentID)
		plan.InsertParentThenChildSlots = buildParentChildSlots(int64(parentCount)+1, 20001)
		plan.InsertParentThenUpdateChildSlots = buildRebindSlots(12001, plan.ExistingParentID, int64(parentCount)+101)
	}

	if len(parentIDs) >= 2 {
		plan.RebindParentID = parentIDs[1]
		plan.RebindChildSlots = buildExistingRebindSlots(13001, plan.ExistingParentID, plan.RebindParentID)
	}

	plan.DeleteParentCascadeSlots = buildCascadeSlots(14001, 15001)

	if len(hotParentIDs) >= 1 {
		plan.ConcurrentHotParentInsertSlots = buildHotChildSlots(16001, hotParentIDs)
	}

	return plan
}

func buildBasicChildSlots(firstChildID, parentID int64) []BasicChildSlot {
	slots := make([]BasicChildSlot, 0, genericFixturePoolSize)
	for i := 0; i < genericFixturePoolSize; i++ {
		slots = append(slots, BasicChildSlot{
			ChildID:  firstChildID + int64(i),
			ParentID: parentID,
		})
	}
	return slots
}

func buildParentChildSlots(firstParentID, firstChildID int64) []ParentChildSlot {
	slots := make([]ParentChildSlot, 0, genericFixturePoolSize)
	for i := 0; i < genericFixturePoolSize; i++ {
		slots = append(slots, ParentChildSlot{
			ParentID: firstParentID + int64(i),
			ChildID:  firstChildID + int64(i),
		})
	}
	return slots
}

func buildExistingRebindSlots(firstChildID, baseParentID, targetParentID int64) []RebindSlot {
	slots := make([]RebindSlot, 0, genericFixturePoolSize)
	for i := 0; i < genericFixturePoolSize; i++ {
		slots = append(slots, RebindSlot{
			ChildID:        firstChildID + int64(i),
			BaseParentID:   baseParentID,
			TargetParentID: targetParentID,
		})
	}
	return slots
}

func buildInsertThenRebindSlots(firstChildID, baseParentID, firstTargetParentID int64) []RebindSlot {
	slots := make([]RebindSlot, 0, genericFixturePoolSize)
	for i := 0; i < genericFixturePoolSize; i++ {
		slots = append(slots, RebindSlot{
			ChildID:        firstChildID + int64(i),
			BaseParentID:   baseParentID,
			TargetParentID: firstTargetParentID + int64(i),
		})
	}
	return slots
}

func buildRebindSlots(firstChildID, baseParentID, firstTargetParentID int64) []RebindSlot {
	return buildInsertThenRebindSlots(firstChildID, baseParentID, firstTargetParentID)
}

func buildCascadeSlots(firstParentID, firstChildID int64) []CascadeSlot {
	slots := make([]CascadeSlot, 0, genericFixturePoolSize)
	for i := 0; i < genericFixturePoolSize; i++ {
		slots = append(slots, CascadeSlot{
			ParentID: firstParentID + int64(i),
			ChildID:  firstChildID + int64(i),
		})
	}
	return slots
}

func buildHotChildSlots(firstChildID int64, hotParentIDs []int64) []HotChildSlot {
	slots := make([]HotChildSlot, 0, genericFixturePoolSize)
	for i := 0; i < genericFixturePoolSize; i++ {
		slots = append(slots, HotChildSlot{
			ChildID:  firstChildID + int64(i),
			ParentID: hotParentIDs[i%len(hotParentIDs)],
		})
	}
	return slots
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
