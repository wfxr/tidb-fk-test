package seed

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	dbpkg "github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/db"
)

func TestReadPreparedMetadataReturnsPersistedSeedInputs(t *testing.T) {
	queryer := stubMetadataQueryer{
		row: stubMetadataRow{
			values: []any{int64(seedPlanVersion), int64(12), int64(3), []byte("2026-05-25 14:00:00.000000")},
		},
	}

	got, err := ReadPreparedMetadata(context.Background(), queryer)
	if err != nil {
		t.Fatalf("ReadPreparedMetadata() error = %v", err)
	}

	if got.SeedPlanVersion != seedPlanVersion {
		t.Fatalf("SeedPlanVersion = %d, want %d", got.SeedPlanVersion, seedPlanVersion)
	}
	if got.SeedParentRowsPerTable != 12 {
		t.Fatalf("SeedParentRowsPerTable = %d, want 12", got.SeedParentRowsPerTable)
	}
	if got.SeedHotParentKeys != 3 {
		t.Fatalf("SeedHotParentKeys = %d, want 3", got.SeedHotParentKeys)
	}
	wantPreparedAt := time.Date(2026, time.May, 25, 14, 0, 0, 0, time.UTC)
	if !got.PreparedAt.Equal(wantPreparedAt) {
		t.Fatalf("PreparedAt = %v, want %v", got.PreparedAt, wantPreparedAt)
	}
}

func TestReadPreparedMetadataReturnsNotFoundWhenNoRowExists(t *testing.T) {
	queryer := stubMetadataQueryer{
		row: stubMetadataRow{err: sql.ErrNoRows},
	}

	_, err := ReadPreparedMetadata(context.Background(), queryer)
	if !errors.Is(err, ErrPrepareMetadataNotFound) {
		t.Fatalf("ReadPreparedMetadata() error = %v, want %v", err, ErrPrepareMetadataNotFound)
	}
}

type stubMetadataQueryer struct {
	row stubMetadataRow
}

func (s stubMetadataQueryer) QueryRowContext(context.Context, string, ...any) dbpkg.RowScanner {
	return s.row
}

type stubMetadataRow struct {
	values []any
	err    error
}

func (r stubMetadataRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		switch ptr := dest[i].(type) {
		case *int:
			*ptr = int(r.values[i].(int64))
		case *sql.NullString:
			switch value := r.values[i].(type) {
			case []byte:
				*ptr = sql.NullString{String: string(value), Valid: true}
			case string:
				*ptr = sql.NullString{String: value, Valid: true}
			case time.Time:
				*ptr = sql.NullString{String: value.UTC().Format("2006-01-02 15:04:05.999999"), Valid: true}
			default:
				return errors.New("unsupported null string source")
			}
		default:
			return errors.New("unsupported scan destination")
		}
	}
	return nil
}
