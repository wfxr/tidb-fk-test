package seed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	dbpkg "github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/db"
)

const (
	prepareMetadataTable       = "fk_prepare_metadata"
	SeedPlanVersion            = 1
	seedPlanVersion            = SeedPlanVersion
	phaseApplyMetadataSchema   = "apply_metadata_schema"
	phaseRecordPrepareMetadata = "record_prepare_metadata"
)

const createPrepareMetadataTableStatement = "CREATE TABLE IF NOT EXISTS fk_prepare_metadata (singleton_id TINYINT PRIMARY KEY, seed_plan_version INT NOT NULL, seed_parent_rows_per_table INT NOT NULL, seed_hot_parent_keys INT NOT NULL, prepared_at DATETIME(6) NOT NULL)"

type PreparedMetadata struct {
	SeedPlanVersion        int
	SeedParentRowsPerTable int
	SeedHotParentKeys      int
	PreparedAt             time.Time
}

var ErrPrepareMetadataNotFound = errors.New("prepare metadata not found")

type metadataReader interface {
	dbpkg.Queryer
}

func ReadPreparedMetadata(ctx context.Context, db metadataReader) (PreparedMetadata, error) {
	var metadata PreparedMetadata
	var preparedAt sql.NullString
	err := db.QueryRowContext(
		ctx,
		"SELECT seed_plan_version, seed_parent_rows_per_table, seed_hot_parent_keys, prepared_at FROM fk_prepare_metadata WHERE singleton_id = 1",
	).Scan(
		&metadata.SeedPlanVersion,
		&metadata.SeedParentRowsPerTable,
		&metadata.SeedHotParentKeys,
		&preparedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PreparedMetadata{}, ErrPrepareMetadataNotFound
		}
		return PreparedMetadata{}, err
	}
	if preparedAt.Valid {
		parsed, err := parsePreparedAt(preparedAt.String)
		if err != nil {
			return PreparedMetadata{}, fmt.Errorf("parse prepared_at: %w", err)
		}
		metadata.PreparedAt = parsed
	}
	return metadata, nil
}

func parsePreparedAt(value string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported datetime format %q", value)
}

func ValidatePreparedState(ctx context.Context, db metadataReader, applied AppliedState) error {
	checks := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name:  "prepare metadata row",
			query: "SELECT COUNT(*) FROM fk_prepare_metadata WHERE singleton_id = 1",
		},
		{
			name:  "probe summary row",
			query: "SELECT COUNT(*) FROM probe_summary WHERE singleton_id = 1",
		},
	}
	if applied.Generic.ExistingParentID != 0 {
		checks = append(checks, struct {
			name  string
			query string
			args  []any
		}{
			name:  "generic seed parent",
			query: "SELECT COUNT(*) FROM parent_basic WHERE id = ?",
			args:  []any{applied.Generic.ExistingParentID},
		})
	}
	if applied.Billing.ExistingBillID != 0 {
		checks = append(checks, struct {
			name  string
			query string
			args  []any
		}{
			name:  "billing seed bill",
			query: "SELECT COUNT(*) FROM bill WHERE id = ?",
			args:  []any{applied.Billing.ExistingBillID},
		})
	}

	for _, check := range checks {
		var count int64
		if err := db.QueryRowContext(ctx, check.query, check.args...).Scan(&count); err != nil {
			return fmt.Errorf("%s: %w", check.name, err)
		}
		if count == 0 {
			return fmt.Errorf("%s is missing; run prepare first", check.name)
		}
	}

	return nil
}
