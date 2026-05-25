package seed

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"
)

const (
	phaseApplyGenericSchema     = "apply_generic_schema"
	phaseApplyPropertyMeSchema  = "apply_propertyme_schema"
	phaseSeedGenericFixtures    = "seed_generic_fixtures"
	phaseSeedPropertyMeFixtures = "seed_propertyme_fixtures"
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
	name       string
	statements []applyStatement
}

type applyStatement struct {
	query string
	args  []any
}

func ApplyAll(ctx context.Context, db execer, cfg config.Config) (AppliedState, error) {
	applied := AppliedState{
		Generic:    GenericPlan(cfg),
		PropertyMe: PropertyMePlan(cfg),
	}

	for _, phase := range buildApplyPhases(applied) {
		for _, stmt := range phase.statements {
			if _, err := db.ExecContext(ctx, stmt.query, stmt.args...); err != nil {
				return applied, fmt.Errorf("%s: %w", phase.name, err)
			}
		}
		applied.CompletedPhases = append(applied.CompletedPhases, phase.name)
	}

	return applied, nil
}

func buildApplyPhases(applied AppliedState) []applyPhase {
	return []applyPhase{
		{
			name:       phaseApplyGenericSchema,
			statements: genericSchemaStatements(),
		},
		{
			name:       phaseApplyPropertyMeSchema,
			statements: propertyMeSchemaStatements(),
		},
		{
			name:       phaseSeedGenericFixtures,
			statements: genericFixtureStatements(applied.Generic),
		},
		{
			name:       phaseSeedPropertyMeFixtures,
			statements: propertyMeFixtureStatements(applied.PropertyMe),
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
