package seed

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/config"
)

const (
	phaseApplyGenericSchema  = "apply_generic_schema"
	phaseApplyBillingSchema  = "apply_billing_schema"
	phaseSeedGenericFixtures = "seed_generic_fixtures"
	phaseSeedBillingFixtures = "seed_billing_fixtures"
)

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type AppliedState struct {
	Generic         GenericSeedPlan
	Billing         BillingSeedPlan
	CompletedPhases []string
}

type applyPhase struct {
	name       string
	statements []applyStatement
}

type applyStatement struct {
	query string
	args  []any
}

func ApplyAll(ctx context.Context, db execer, cfg config.Config) (AppliedState, error) {
	applied := BuildAppliedState(cfg)

	for _, phase := range buildApplyPhases(applied) {
		for _, stmt := range phase.statements {
			if _, err := db.ExecContext(ctx, stmt.query, stmt.args...); err != nil {
				return applied, fmt.Errorf("%s: %w", phase.name, err)
			}
		}
		applied.CompletedPhases = append(applied.CompletedPhases, phase.name)
	}

	if _, err := db.ExecContext(
		ctx,
		"INSERT INTO fk_prepare_metadata (singleton_id, seed_plan_version, seed_parent_rows_per_table, seed_hot_parent_keys, prepared_at) VALUES (1, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE seed_plan_version = VALUES(seed_plan_version), seed_parent_rows_per_table = VALUES(seed_parent_rows_per_table), seed_hot_parent_keys = VALUES(seed_hot_parent_keys), prepared_at = VALUES(prepared_at)",
		seedPlanVersion,
		cfg.SeedParentRowsPerTable,
		cfg.SeedHotParentKeys,
		time.Now().UTC(),
	); err != nil {
		return applied, fmt.Errorf("%s: %w", phaseRecordPrepareMetadata, err)
	}
	applied.CompletedPhases = append(applied.CompletedPhases, phaseRecordPrepareMetadata)

	return applied, nil
}

func BuildAppliedState(cfg config.Config) AppliedState {
	return AppliedState{
		Generic: GenericPlan(cfg),
		Billing: BillingPlan(cfg),
	}
}

func buildApplyPhases(applied AppliedState) []applyPhase {
	return []applyPhase{
		{
			name: phaseApplyMetadataSchema,
			statements: []applyStatement{
				{query: createPrepareMetadataTableStatement},
			},
		},
		{
			name:       phaseApplyGenericSchema,
			statements: genericSchemaStatements(),
		},
		{
			name:       phaseApplyBillingSchema,
			statements: billingSchemaStatements(),
		},
		{
			name:       phaseSeedGenericFixtures,
			statements: genericFixtureStatements(applied.Generic),
		},
		{
			name:       phaseSeedBillingFixtures,
			statements: billingFixtureStatements(applied.Billing),
		},
	}
}

func newInsertStatement(table string, columns, updateColumns []string, rows [][]any) (applyStatement, bool) {
	if len(rows) == 0 {
		return applyStatement{}, false
	}

	var builder strings.Builder
	builder.WriteString("INSERT INTO ")
	builder.WriteString(table)
	builder.WriteString(" (")
	builder.WriteString(strings.Join(columns, ", "))
	builder.WriteString(") VALUES ")

	args := make([]any, 0, len(rows)*len(columns))
	for rowIndex, row := range rows {
		if len(row) != len(columns) {
			panic(fmt.Sprintf("seed row for %s has %d values, want %d", table, len(row), len(columns)))
		}
		if rowIndex > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString("(")
		for valueIndex := range row {
			if valueIndex > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString("?")
		}
		builder.WriteString(")")
		args = append(args, row...)
	}

	if len(updateColumns) > 0 {
		builder.WriteString(" ON DUPLICATE KEY UPDATE ")
		for index, column := range updateColumns {
			if index > 0 {
				builder.WriteString(", ")
			}
			builder.WriteString(column)
			builder.WriteString(" = VALUES(")
			builder.WriteString(column)
			builder.WriteString(")")
		}
	}

	return applyStatement{
		query: builder.String(),
		args:  args,
	}, true
}
