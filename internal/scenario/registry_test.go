package scenario

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/db"
)

var _ db.Session = fakeSession{}
var _ db.Tx = fakeTx{}
var _ db.TxSession = fakeTxSession{}

type fakeSession struct{}

func (fakeSession) BeginTx(context.Context, *sql.TxOptions) (db.Tx, error) {
	return fakeTx{}, nil
}

type fakeTxSession struct{}

func (fakeTxSession) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (fakeTxSession) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

type fakeTx struct {
	fakeTxSession
}

func (fakeTx) Commit() error {
	return nil
}

func (fakeTx) Rollback() error {
	return nil
}

func TestRegistryContainsFailureProbe(t *testing.T) {
	reg := NewRegistry()

	s, ok := reg.Get("payment_bill_update_probe")
	if !ok {
		t.Fatalf("payment_bill_update_probe not registered")
	}

	meta := s.Meta()
	if meta.Group != FailureProbeGroup {
		t.Fatalf("Group = %q, want %q", meta.Group, FailureProbeGroup)
	}
	if meta.ExpectedErrorMatch == "" {
		t.Fatal("ExpectedErrorMatch should not be empty")
	}
}

func TestRegistryRegistersPlannedPlaceholderScenarios(t *testing.T) {
	reg := NewRegistry()

	want := map[string]Group{
		"generic_insert_existing_parent":             GenericGroup,
		"generic_insert_parent_then_child":           GenericGroup,
		"generic_update_child_no_fk_change":          GenericGroup,
		"generic_rebind_child_fk":                    GenericGroup,
		"generic_insert_parent_then_update_child_fk": GenericGroup,
		"generic_delete_parent_cascade":              GenericGroup,
		"generic_concurrent_hot_parent_insert":       GenericGroup,
		"payment_bill_update_probe":                  FailureProbeGroup,
		"pm_journal_posting_bill":                    PropertyMeGroup,
		"pm_folio_balance_update":                    PropertyMeGroup,
		"pm_fk_backfill":                             PropertyMeGroup,
		"pm_payment_mixed_references":                PropertyMeGroup,
		"pm_cascade_path":                            PropertyMeGroup,
	}

	if got := len(reg.All()); got != len(want) {
		t.Fatalf("len(All()) = %d, want %d", got, len(want))
	}

	for name, group := range want {
		s, ok := reg.Get(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}

		meta := s.Meta()
		if meta.Name != name {
			t.Fatalf("Meta().Name = %q, want %q", meta.Name, name)
		}
		if meta.Group != group {
			t.Fatalf("%s Group = %q, want %q", name, meta.Group, group)
		}
	}
}

func TestMetadataMatchesExpectedError(t *testing.T) {
	meta := Metadata{
		ExpectedErrorMatch: "upgrading a shared lock to an exclusive lock is not supported",
	}

	err := errors.New("ERROR 1105 (HY000): upgrading a shared lock to an exclusive lock is not supported")
	if !meta.MatchesExpectedError(err) {
		t.Fatal("MatchesExpectedError() = false, want true")
	}

	if meta.MatchesExpectedError(nil) {
		t.Fatal("MatchesExpectedError(nil) = true, want false")
	}
}
