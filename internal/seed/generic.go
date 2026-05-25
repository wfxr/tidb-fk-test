package seed

import "github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"

const genericFixturePoolSize = 4

const (
	createParentBasicTableStatement   = "CREATE TABLE IF NOT EXISTS parent_basic (id BIGINT PRIMARY KEY, note VARCHAR(255) NOT NULL)"
	createChildBasicTableStatement    = "CREATE TABLE IF NOT EXISTS child_basic (id BIGINT PRIMARY KEY, parent_id BIGINT NOT NULL, payload VARCHAR(255) NOT NULL, CONSTRAINT fk_child_basic_parent FOREIGN KEY (parent_id) REFERENCES parent_basic(id))"
	createChildRebindTableStatement   = "CREATE TABLE IF NOT EXISTS child_rebind (id BIGINT PRIMARY KEY, parent_id BIGINT NOT NULL, payload VARCHAR(255) NOT NULL, CONSTRAINT fk_child_rebind_parent FOREIGN KEY (parent_id) REFERENCES parent_basic(id))"
	createParentCascadeTableStatement = "CREATE TABLE IF NOT EXISTS parent_cascade (id BIGINT PRIMARY KEY, note VARCHAR(255) NOT NULL)"
	createChildCascadeTableStatement  = "CREATE TABLE IF NOT EXISTS child_cascade (id BIGINT PRIMARY KEY, parent_id BIGINT NOT NULL, payload VARCHAR(255) NOT NULL, CONSTRAINT fk_child_cascade_parent FOREIGN KEY (parent_id) REFERENCES parent_cascade(id) ON DELETE CASCADE)"
	createParentHotTableStatement     = "CREATE TABLE IF NOT EXISTS parent_hot (id BIGINT PRIMARY KEY, note VARCHAR(255) NOT NULL)"
	createChildHotTableStatement      = "CREATE TABLE IF NOT EXISTS child_hot (id BIGINT PRIMARY KEY, parent_id BIGINT NOT NULL, payload VARCHAR(255) NOT NULL, CONSTRAINT fk_child_hot_parent FOREIGN KEY (parent_id) REFERENCES parent_hot(id))"
	createProbeSummaryTableStatement  = "CREATE TABLE IF NOT EXISTS probe_summary (singleton_id TINYINT PRIMARY KEY, expected_fk_failure BIGINT NOT NULL, unexpected_failure BIGINT NOT NULL)"
)

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

func genericSchemaStatements() []applyStatement {
	return []applyStatement{
		{query: createParentBasicTableStatement},
		{query: createChildBasicTableStatement},
		{query: createChildRebindTableStatement},
		{query: createParentCascadeTableStatement},
		{query: createChildCascadeTableStatement},
		{query: createParentHotTableStatement},
		{query: createChildHotTableStatement},
		{query: createProbeSummaryTableStatement},
	}
}

func genericFixtureStatements(plan GenericSeedPlan) []applyStatement {
	var statements []applyStatement

	if stmt, ok := newInsertStatement(
		"parent_basic",
		[]string{"id", "note"},
		[]string{"note"},
		genericParentBasicRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"child_basic",
		[]string{"id", "parent_id", "payload"},
		[]string{"parent_id", "payload"},
		genericChildBasicRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"child_rebind",
		[]string{"id", "parent_id", "payload"},
		[]string{"parent_id", "payload"},
		genericChildRebindRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"parent_cascade",
		[]string{"id", "note"},
		[]string{"note"},
		genericParentCascadeRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"child_cascade",
		[]string{"id", "parent_id", "payload"},
		[]string{"parent_id", "payload"},
		genericChildCascadeRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"parent_hot",
		[]string{"id", "note"},
		[]string{"note"},
		genericParentHotRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"child_hot",
		[]string{"id", "parent_id", "payload"},
		[]string{"parent_id", "payload"},
		genericChildHotRows(plan),
	); ok {
		statements = append(statements, stmt)
	}

	if stmt, ok := newInsertStatement(
		"probe_summary",
		[]string{"singleton_id", "expected_fk_failure", "unexpected_failure"},
		[]string{"expected_fk_failure", "unexpected_failure"},
		genericProbeSummaryRows(),
	); ok {
		statements = append(statements, stmt)
	}

	return statements
}

func genericParentBasicRows(plan GenericSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.ParentIDs))
	for _, parentID := range plan.ParentIDs {
		rows = append(rows, []any{parentID, "seed-parent-basic"})
	}
	return rows
}

func genericChildBasicRows(plan GenericSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.InsertExistingParentSlots)+len(plan.UpdateChildNoFKChangeSlots))
	for _, slot := range plan.InsertExistingParentSlots {
		rows = append(rows, []any{slot.ChildID, slot.ParentID, "seed-child-basic-insert"})
	}
	for _, slot := range plan.UpdateChildNoFKChangeSlots {
		rows = append(rows, []any{slot.ChildID, slot.ParentID, "seed-child-basic-update"})
	}
	return rows
}

func genericChildRebindRows(plan GenericSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.RebindChildSlots)+len(plan.InsertParentThenUpdateChildSlots))
	for _, slot := range plan.RebindChildSlots {
		rows = append(rows, []any{slot.ChildID, slot.BaseParentID, "seed-child-rebind"})
	}
	for _, slot := range plan.InsertParentThenUpdateChildSlots {
		rows = append(rows, []any{slot.ChildID, slot.BaseParentID, "seed-child-rebind-insert"})
	}
	return rows
}

func genericParentCascadeRows(plan GenericSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.DeleteParentCascadeSlots))
	for _, slot := range plan.DeleteParentCascadeSlots {
		rows = append(rows, []any{slot.ParentID, "seed-parent-cascade"})
	}
	return rows
}

func genericChildCascadeRows(plan GenericSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.DeleteParentCascadeSlots))
	for _, slot := range plan.DeleteParentCascadeSlots {
		rows = append(rows, []any{slot.ChildID, slot.ParentID, "seed-child-cascade"})
	}
	return rows
}

func genericParentHotRows(plan GenericSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.HotParentIDs))
	for _, parentID := range plan.HotParentIDs {
		rows = append(rows, []any{parentID, "seed-parent-hot"})
	}
	return rows
}

func genericChildHotRows(plan GenericSeedPlan) [][]any {
	rows := make([][]any, 0, len(plan.ConcurrentHotParentInsertSlots))
	for _, slot := range plan.ConcurrentHotParentInsertSlots {
		rows = append(rows, []any{slot.ChildID, slot.ParentID, "seed-child-hot"})
	}
	return rows
}

func genericProbeSummaryRows() [][]any {
	return [][]any{{int64(1), int64(0), int64(0)}}
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
