package scenario

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/db"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/seed"
)

var _ Scenario = NewPMJournalPostingBill()
var _ Scenario = NewPMFolioBalanceUpdate()
var _ Scenario = NewPMFKBackfill()
var _ Scenario = NewPMPaymentMixedReferences()
var _ Scenario = NewPMCascadePath()

type propertyMeCall struct {
	method string
	query  string
	args   []any
}

type recordingPropertyMeRow struct {
	scanCalls int
	err       error
}

func (r *recordingPropertyMeRow) Scan(dest ...any) error {
	r.scanCalls++
	if r.err != nil {
		return r.err
	}
	for _, item := range dest {
		ptr, ok := item.(*int64)
		if ok {
			*ptr = 0
		}
	}
	return nil
}

type recordingPropertyMeSession struct {
	calls        []propertyMeCall
	queryRowErr  error
	lastQueryRow *recordingPropertyMeRow
}

func (s *recordingPropertyMeSession) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.calls = append(s.calls, propertyMeCall{
		method: "exec",
		query:  query,
		args:   append([]any(nil), args...),
	})
	return nil, nil
}

func (s *recordingPropertyMeSession) QueryRowContext(_ context.Context, query string, args ...any) db.RowScanner {
	s.calls = append(s.calls, propertyMeCall{
		method: "query",
		query:  query,
		args:   append([]any(nil), args...),
	})
	s.lastQueryRow = &recordingPropertyMeRow{err: s.queryRowErr}
	return s.lastQueryRow
}

func TestPropertyMeScenarioMetadata(t *testing.T) {
	tests := []struct {
		name string
		got  Scenario
	}{
		{name: "pm_journal_posting_bill", got: NewPMJournalPostingBill()},
		{name: "pm_folio_balance_update", got: NewPMFolioBalanceUpdate()},
		{name: "pm_fk_backfill", got: NewPMFKBackfill()},
		{name: "pm_payment_mixed_references", got: NewPMPaymentMixedReferences()},
		{name: "pm_cascade_path", got: NewPMCascadePath()},
	}

	for _, tt := range tests {
		meta := tt.got.Meta()
		if meta.Name != tt.name {
			t.Fatalf("%T Meta().Name = %q, want %q", tt.got, meta.Name, tt.name)
		}
		if meta.Group != PropertyMeGroup {
			t.Fatalf("%s Meta().Group = %q, want %q", tt.name, meta.Group, PropertyMeGroup)
		}
	}
}

func TestPMJournalPostingBillOrder(t *testing.T) {
	sess := &recordingPropertyMeSession{}
	seedState := testPropertyMeSeedState()
	slot := seedState.PropertyMe.JournalPostingBillSlots[0]

	err := NewPMJournalPostingBill().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM bill WHERE id = ?",
		"DELETE FROM posting WHERE id = ?",
		"DELETE FROM journal WHERE id = ?",
		"INSERT INTO journal (id, customer_id, folio_id, member_id, reference, amount_cents) VALUES (?, ?, ?, ?, ?, ?)",
		"INSERT INTO posting (id, journal_id, status, amount_cents) VALUES (?, ?, ?, ?)",
		"INSERT INTO bill (id, journal_id, folio_id, status, total_cents, paid_cents) VALUES (?, ?, ?, ?, ?, ?)",
	}
	if got := propertyMeQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.BillID},
		{slot.PostingID},
		{slot.JournalID},
		{slot.JournalID, slot.CustomerID, slot.FolioID, nil, "pm-journal", int64(1500)},
		{slot.PostingID, slot.JournalID, "posted", int64(1500)},
		{slot.BillID, slot.JournalID, slot.FolioID, "open", int64(1500), int64(0)},
	}
	if got := propertyMeArgs(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestPMFolioBalanceUpdateUsesForUpdateThenUpdate(t *testing.T) {
	sess := &recordingPropertyMeSession{}
	seedState := testPropertyMeSeedState()
	slot := seedState.PropertyMe.FolioBalanceUpdateSlots[0]

	err := NewPMFolioBalanceUpdate().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantMethods := []string{"query", "exec"}
	if got := propertyMeMethods(sess.calls); !reflect.DeepEqual(got, wantMethods) {
		t.Fatalf("methods = %v, want %v", got, wantMethods)
	}

	wantQueries := []string{
		"SELECT id FROM foliobalance WHERE id = ? FOR UPDATE",
		"UPDATE foliobalance SET balance_cents = balance_cents + ?, updated_count = updated_count + 1 WHERE id = ?",
	}
	if got := propertyMeQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.FolioBalanceID},
		{int64(250), slot.FolioBalanceID},
	}
	if got := propertyMeArgs(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
	if sess.lastQueryRow == nil {
		t.Fatal("QueryRowContext() was not called")
	}
	if got := sess.lastQueryRow.scanCalls; got != 1 {
		t.Fatalf("scanCalls = %d, want 1", got)
	}
}

func TestPMFolioBalanceUpdatePropagatesForUpdateScanFailure(t *testing.T) {
	wantErr := errors.New("for update lock failed")
	sess := &recordingPropertyMeSession{queryRowErr: wantErr}

	err := NewPMFolioBalanceUpdate().Run(context.Background(), sess, testPropertyMeSeedState())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}

	wantQueries := []string{
		"SELECT id FROM foliobalance WHERE id = ? FOR UPDATE",
	}
	if got := propertyMeQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}
}

func TestPMFKBackfillOrder(t *testing.T) {
	sess := &recordingPropertyMeSession{}
	seedState := testPropertyMeSeedState()
	slot := seedState.PropertyMe.FKBackfillSlots[0]

	err := NewPMFKBackfill().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM feelog WHERE id = ?",
		"DELETE FROM withdrawal WHERE id = ?",
		"DELETE FROM bill WHERE id = ?",
		"DELETE FROM statement WHERE id = ?",
		"INSERT INTO feelog (id, journal_id, fee_bill_id, amount_cents) VALUES (?, ?, ?, ?)",
		"INSERT INTO withdrawal (id, journal_id, statement_id, status, amount_cents) VALUES (?, ?, ?, ?, ?)",
		"INSERT INTO bill (id, journal_id, folio_id, status, total_cents, paid_cents) VALUES (?, ?, ?, ?, ?, ?)",
		"UPDATE feelog SET fee_bill_id = ? WHERE id = ?",
		"INSERT INTO statement (id, customer_id, folio_id, status, balance_cents) VALUES (?, ?, ?, ?, ?)",
		"UPDATE folio SET last_statement_id = ? WHERE id = ?",
		"UPDATE withdrawal SET statement_id = ? WHERE id = ?",
	}
	if got := propertyMeQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.FeeLogID},
		{slot.WithdrawalID},
		{slot.BillID},
		{slot.StatementID},
		{slot.FeeLogID, slot.JournalID, nil, int64(225)},
		{slot.WithdrawalID, slot.JournalID, nil, "pending", int64(300)},
		{slot.BillID, slot.JournalID, slot.FolioID, "open", int64(225), int64(0)},
		{slot.BillID, slot.FeeLogID},
		{slot.StatementID, slot.CustomerID, slot.FolioID, "issued", int64(525)},
		{slot.StatementID, slot.FolioID},
		{slot.StatementID, slot.WithdrawalID},
	}
	if got := propertyMeArgs(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestPMPaymentMixedReferencesOrder(t *testing.T) {
	sess := &recordingPropertyMeSession{}
	seedState := testPropertyMeSeedState()
	slot := seedState.PropertyMe.PaymentMixedReferenceSlots[0]

	err := NewPMPaymentMixedReferences().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM payment WHERE id = ?",
		"DELETE FROM journal WHERE id = ?",
		"INSERT INTO journal (id, customer_id, folio_id, member_id, reference, amount_cents) VALUES (?, ?, ?, ?, ?, ?)",
		"INSERT INTO payment (id, bill_id, journal_id, status, amount_cents) VALUES (?, ?, ?, ?, ?)",
	}
	if got := propertyMeQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.PaymentID},
		{slot.JournalID},
		{slot.JournalID, slot.CustomerID, slot.FolioID, nil, "pm-payment", int64(875)},
		{slot.PaymentID, slot.BillID, slot.JournalID, "applied", int64(875)},
	}
	if got := propertyMeArgs(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestPMCascadePathOrder(t *testing.T) {
	sess := &recordingPropertyMeSession{}
	seedState := testPropertyMeSeedState()
	slot := seedState.PropertyMe.CascadePathSlots[0]

	err := NewPMCascadePath().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM withdrawal WHERE id = ?",
		"DELETE FROM statement WHERE id = ?",
		"INSERT INTO statement (id, customer_id, folio_id, status, balance_cents) VALUES (?, ?, ?, ?, ?)",
		"INSERT INTO withdrawal (id, journal_id, statement_id, status, amount_cents) VALUES (?, ?, ?, ?, ?)",
		"DELETE FROM statement WHERE id = ?",
	}
	if got := propertyMeQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.WithdrawalID},
		{slot.StatementID},
		{slot.StatementID, slot.CustomerID, slot.FolioID, "issued", int64(640)},
		{slot.WithdrawalID, slot.JournalID, slot.StatementID, "pending", int64(640)},
		{slot.StatementID},
	}
	if got := propertyMeArgs(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestPropertyMeScenarioRequiresRuntimeSeedPlan(t *testing.T) {
	sess := &recordingPropertyMeSession{}

	err := NewPMJournalPostingBill().Run(context.Background(), sess, SeedState{})
	if err == nil {
		t.Fatal("Run() error = nil, want missing propertyme seed plan error")
	}
	if !strings.Contains(err.Error(), "propertyme seed plan required") {
		t.Fatalf("Run() error = %v, want missing propertyme seed plan error", err)
	}
	if got := len(sess.calls); got != 0 {
		t.Fatalf("len(calls) = %d, want 0", got)
	}
}

func propertyMeMethods(calls []propertyMeCall) []string {
	methods := make([]string, 0, len(calls))
	for _, call := range calls {
		methods = append(methods, call.method)
	}
	return methods
}

func propertyMeQueries(calls []propertyMeCall) []string {
	queries := make([]string, 0, len(calls))
	for _, call := range calls {
		queries = append(queries, call.query)
	}
	return queries
}

func propertyMeArgs(calls []propertyMeCall) [][]any {
	args := make([][]any, 0, len(calls))
	for _, call := range calls {
		args = append(args, call.args)
	}
	return args
}

func testPropertyMeSeedState() SeedState {
	return SeedState{
		PropertyMe: seed.PropertyMeSeedPlan{
			CustomerIDs: []int64{1, 2},
			FolioIDs:    []int64{1001, 1002},

			ExistingCustomerID: 1,
			ExistingFolioID:    1001,
			ExistingJournalID:  3001,
			ExistingBillID:     4001,

			JournalPostingBillSlots: []seed.JournalPostingBillSlot{
				{CustomerID: 1, FolioID: 1001, JournalID: 10001, PostingID: 11001, BillID: 12001},
				{CustomerID: 2, FolioID: 1002, JournalID: 10002, PostingID: 11002, BillID: 12002},
			},
			FolioBalanceUpdateSlots: []seed.FolioBalanceUpdateSlot{
				{CustomerID: 1, FolioID: 1001, FolioBalanceID: 13001},
				{CustomerID: 2, FolioID: 1002, FolioBalanceID: 13002},
			},
			FKBackfillSlots: []seed.FKBackfillSlot{
				{CustomerID: 1, FolioID: 1001, JournalID: 14001, BillID: 15001, FeeLogID: 16001, StatementID: 17001, WithdrawalID: 18001},
				{CustomerID: 2, FolioID: 1002, JournalID: 14002, BillID: 15002, FeeLogID: 16002, StatementID: 17002, WithdrawalID: 18002},
			},
			PaymentMixedReferenceSlots: []seed.PaymentMixedReferenceSlot{
				{CustomerID: 1, FolioID: 1001, BillID: 4001, JournalID: 19001, PaymentID: 20001},
				{CustomerID: 2, FolioID: 1002, BillID: 4001, JournalID: 19002, PaymentID: 20002},
			},
			CascadePathSlots: []seed.CascadePathSlot{
				{CustomerID: 1, FolioID: 1001, JournalID: 3001, StatementID: 21001, WithdrawalID: 22001},
				{CustomerID: 2, FolioID: 1002, JournalID: 3001, StatementID: 21002, WithdrawalID: 22002},
			},
			PaymentBillUpdateProbeSlots: []seed.PaymentBillUpdateProbeSlot{
				{CustomerID: 1, FolioID: 1001, BillID: 4001, JournalID: 3001, PaymentID: 18001},
				{CustomerID: 2, FolioID: 1002, BillID: 4001, JournalID: 3001, PaymentID: 18002},
			},
		},
	}
}
