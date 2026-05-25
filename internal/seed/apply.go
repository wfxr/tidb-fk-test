package seed

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"
)

const (
	phaseApplyGenericSchema     = "apply_generic_schema"
	phaseApplyPropertyMeSchema  = "apply_propertyme_schema"
	phaseSeedGenericFixtures    = "seed_generic_fixtures"
	phaseSeedPropertyMeFixtures = "seed_propertyme_fixtures"

	applyGenericSchemaStatement     = "SELECT 1 /* seed:apply_generic_schema */"
	applyPropertyMeSchemaStatement  = "SELECT 1 /* seed:apply_propertyme_schema */"
	seedGenericFixturesStatement    = "SELECT ?, ?, ? /* seed:seed_generic_fixtures */"
	seedPropertyMeFixturesStatement = "SELECT ?, ?, ? /* seed:seed_propertyme_fixtures */"
)

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type AppliedState struct {
	Generic         GenericSeedPlan
	PropertyMe      PropertyMeSeedPlan
	CompletedPhases []string
}

type applyPhase struct {
	name      string
	statement string
	args      []any
}

func ApplyAll(ctx context.Context, db execer, cfg config.Config) (AppliedState, error) {
	applied := AppliedState{
		Generic:    GenericPlan(cfg),
		PropertyMe: PropertyMePlan(cfg),
	}

	for _, phase := range buildApplyPhases(applied) {
		if _, err := db.ExecContext(ctx, phase.statement, phase.args...); err != nil {
			return applied, fmt.Errorf("%s: %w", phase.name, err)
		}
		applied.CompletedPhases = append(applied.CompletedPhases, phase.name)
	}

	return applied, nil
}

func buildApplyPhases(applied AppliedState) []applyPhase {
	return []applyPhase{
		{
			name:      phaseApplyGenericSchema,
			statement: applyGenericSchemaStatement,
		},
		{
			name:      phaseApplyPropertyMeSchema,
			statement: applyPropertyMeSchemaStatement,
		},
		{
			name:      phaseSeedGenericFixtures,
			statement: seedGenericFixturesStatement,
			args: []any{
				len(applied.Generic.ParentIDs),
				len(applied.Generic.DeleteParentCascadeSlots),
				len(applied.Generic.ConcurrentHotParentInsertSlots),
			},
		},
		{
			name:      phaseSeedPropertyMeFixtures,
			statement: seedPropertyMeFixturesStatement,
			args: []any{
				len(applied.PropertyMe.CustomerIDs),
				len(applied.PropertyMe.JournalPostingBillSlots),
				len(applied.PropertyMe.PaymentBillUpdateProbeSlots),
			},
		},
	}
}
