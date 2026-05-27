package checker

import (
	"context"
	"fmt"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/report"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/seed"
)

type CheckKind string

const (
	OrphanChildCheck CheckKind = "orphan_child"
	CascadeCheck     CheckKind = "cascade"
)

var defaultCheckOrder = []string{
	"generic_orphan_child",
	"cascade_result",
}

var defaultChecks = map[string]Definition{
	"generic_orphan_child": {
		Name:  "generic_orphan_child",
		Kind:  OrphanChildCheck,
		Query: "SELECT COUNT(*) FROM child_basic c LEFT JOIN parent_basic p ON c.parent_id = p.id WHERE p.id IS NULL",
	},
	"cascade_result": {
		Name:  "cascade_result",
		Kind:  CascadeCheck,
		Query: "SELECT COUNT(*) FROM child_cascade WHERE parent_id = ?",
	},
}

type RowScanner interface {
	Scan(dest ...any) error
}

type Queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) RowScanner
}

type Definition struct {
	Name  string
	Kind  CheckKind
	Query string
}

type Outcome struct {
	Name              string    `json:"name"`
	Kind              CheckKind `json:"kind"`
	Passed            bool      `json:"passed"`
	Count             int64     `json:"count,omitempty"`
	ExpectedFKFailure int64     `json:"expected_fk_failure,omitempty"`
	UnexpectedFailure int64     `json:"unexpected_failure,omitempty"`
}

type RuntimeSummary struct {
	Totals    report.Totals            `json:"totals"`
	Scenarios []report.ScenarioSummary `json:"scenarios,omitempty"`
}

type Summary struct {
	Runtime  RuntimeSummary `json:"runtime"`
	Outcomes []Outcome      `json:"outcomes"`
}

func DefaultChecks() map[string]Definition {
	cloned := make(map[string]Definition, len(defaultChecks))
	for name, def := range defaultChecks {
		cloned[name] = def
	}
	return cloned
}

func Run(ctx context.Context, db Queryer, applied seed.AppliedState, runtime *report.Summary) (Summary, error) {
	results := Summary{
		Outcomes: make([]Outcome, 0, len(defaultCheckOrder)),
	}
	if runtime != nil {
		results.Runtime = RuntimeSummary{
			Totals:    runtime.Totals(),
			Scenarios: runtime.Scenarios(),
		}
	}

	for _, name := range defaultCheckOrder {
		outcome, err := runCheck(ctx, db, defaultChecks[name], applied, runtime)
		if err != nil {
			return Summary{}, fmt.Errorf("%s: %w", name, err)
		}
		results.Outcomes = append(results.Outcomes, outcome)
	}

	return results, nil
}

func runCheck(
	ctx context.Context,
	db Queryer,
	def Definition,
	applied seed.AppliedState,
	runtime *report.Summary,
) (Outcome, error) {
	switch def.Kind {
	case OrphanChildCheck, CascadeCheck:
		var count int64
		args := checkArgs(def, applied)
		if err := db.QueryRowContext(ctx, def.Query, args...).Scan(&count); err != nil {
			return Outcome{}, err
		}
		return Outcome{
			Name:   def.Name,
			Kind:   def.Kind,
			Passed: count == 0,
			Count:  count,
		}, nil
	default:
		return Outcome{}, fmt.Errorf("unsupported check kind %q", def.Kind)
	}
}

func checkArgs(def Definition, applied seed.AppliedState) []any {
	if def.Kind != CascadeCheck {
		return nil
	}

	var parentID int64
	if len(applied.Generic.DeleteParentCascadeSlots) > 0 {
		parentID = applied.Generic.DeleteParentCascadeSlots[0].ParentID
	}
	return []any{parentID}
}
