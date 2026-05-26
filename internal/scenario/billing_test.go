package scenario

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/db"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/seed"
)

var _ Scenario = NewBillingJournalPostingBill()
var _ Scenario = NewBillingFolioBalanceUpdate()
var _ Scenario = NewBillingFKBackfill()
var _ Scenario = NewBillingPaymentMixedReferences()
var _ Scenario = NewBillingCascadePath()

type billingCall struct {
	method string
	query  string
	args   []any
}

type recordingBillingRow struct {
	scanCalls int
	err       error
}

func (r *recordingBillingRow) Scan(dest ...any) error {
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

type recordingBillingSession struct {
	calls        []billingCall
	queryRowErr  error
	lastQueryRow *recordingBillingRow
}

func (s *recordingBillingSession) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	s.calls = append(s.calls, billingCall{
		method: "exec",
		query:  query,
		args:   append([]any(nil), args...),
	})
	return nil, nil
}

func (s *recordingBillingSession) QueryRowContext(_ context.Context, query string, args ...any) db.RowScanner {
	s.calls = append(s.calls, billingCall{
		method: "query",
		query:  query,
		args:   append([]any(nil), args...),
	})
	s.lastQueryRow = &recordingBillingRow{err: s.queryRowErr}
	return s.lastQueryRow
}

func TestBillingScenarioMetadata(t *testing.T) {
	tests := []struct {
		name string
		got  Scenario
	}{
		{name: "billing_journal_posting_bill", got: NewBillingJournalPostingBill()},
		{name: "billing_folio_balance_update", got: NewBillingFolioBalanceUpdate()},
		{name: "billing_fk_backfill", got: NewBillingFKBackfill()},
		{name: "billing_payment_mixed_references", got: NewBillingPaymentMixedReferences()},
		{name: "billing_cascade_path", got: NewBillingCascadePath()},
	}

	for _, tt := range tests {
		meta := tt.got.Meta()
		if meta.Name != tt.name {
			t.Fatalf("%T Meta().Name = %q, want %q", tt.got, meta.Name, tt.name)
		}
		if meta.Group != BillingGroup {
			t.Fatalf("%s Meta().Group = %q, want %q", tt.name, meta.Group, BillingGroup)
		}
	}
}

func TestBillingJournalPostingBillOrder(t *testing.T) {
	sess := &recordingBillingSession{}
	seedState := testBillingSeedState()
	slot := seedState.Billing.JournalPostingBillSlots[0]

	err := NewBillingJournalPostingBill().Run(context.Background(), sess, seedState)
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
	if got := billingQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.BillID},
		{slot.PostingID},
		{slot.JournalID},
		{slot.JournalID, slot.CustomerID, slot.FolioID, nil, "billing-journal", int64(1500)},
		{slot.PostingID, slot.JournalID, "posted", int64(1500)},
		{slot.BillID, slot.JournalID, slot.FolioID, "open", int64(1500), int64(0)},
	}
	if got := billingArgs(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestBillingFolioBalanceUpdateUsesForUpdateThenUpdate(t *testing.T) {
	sess := &recordingBillingSession{}
	seedState := testBillingSeedState()
	slot := seedState.Billing.FolioBalanceUpdateSlots[0]

	err := NewBillingFolioBalanceUpdate().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantMethods := []string{"query", "exec"}
	if got := billingMethods(sess.calls); !reflect.DeepEqual(got, wantMethods) {
		t.Fatalf("methods = %v, want %v", got, wantMethods)
	}

	wantQueries := []string{
		"SELECT id FROM foliobalance WHERE id = ? FOR UPDATE",
		"UPDATE foliobalance SET balance_cents = balance_cents + ?, updated_count = updated_count + 1 WHERE id = ?",
	}
	if got := billingQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.FolioBalanceID},
		{int64(250), slot.FolioBalanceID},
	}
	if got := billingArgs(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
	if sess.lastQueryRow == nil {
		t.Fatal("QueryRowContext() was not called")
	}
	if got := sess.lastQueryRow.scanCalls; got != 1 {
		t.Fatalf("scanCalls = %d, want 1", got)
	}
}

func TestBillingFolioBalanceUpdatePropagatesForUpdateScanFailure(t *testing.T) {
	wantErr := errors.New("for update lock failed")
	sess := &recordingBillingSession{queryRowErr: wantErr}

	err := NewBillingFolioBalanceUpdate().Run(context.Background(), sess, testBillingSeedState())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}

	wantQueries := []string{
		"SELECT id FROM foliobalance WHERE id = ? FOR UPDATE",
	}
	if got := billingQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}
}

func TestBillingFKBackfillOrder(t *testing.T) {
	sess := &recordingBillingSession{}
	seedState := testBillingSeedState()
	slot := seedState.Billing.FKBackfillSlots[0]

	err := NewBillingFKBackfill().Run(context.Background(), sess, seedState)
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
		"UPDATE withdrawal SET statement_id = ? WHERE id = ?",
	}
	if got := billingQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
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
		{slot.StatementID, slot.WithdrawalID},
	}
	if got := billingArgs(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestBillingPaymentMixedReferencesOrder(t *testing.T) {
	sess := &recordingBillingSession{}
	seedState := testBillingSeedState()
	slot := seedState.Billing.PaymentMixedReferenceSlots[0]

	err := NewBillingPaymentMixedReferences().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM payment WHERE id = ?",
		"DELETE FROM journal WHERE id = ?",
		"INSERT INTO journal (id, customer_id, folio_id, member_id, reference, amount_cents) VALUES (?, ?, ?, ?, ?, ?)",
		"INSERT INTO payment (id, bill_id, journal_id, status, amount_cents) VALUES (?, ?, ?, ?, ?)",
	}
	if got := billingQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.PaymentID},
		{slot.JournalID},
		{slot.JournalID, slot.CustomerID, slot.FolioID, nil, "billing-payment", int64(875)},
		{slot.PaymentID, slot.BillID, slot.JournalID, "applied", int64(875)},
	}
	if got := billingArgs(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestBillingCascadePathOrder(t *testing.T) {
	sess := &recordingBillingSession{}
	seedState := testBillingSeedState()
	slot := seedState.Billing.CascadePathSlots[0]

	err := NewBillingCascadePath().Run(context.Background(), sess, seedState)
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
	if got := billingQueries(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.WithdrawalID},
		{slot.StatementID},
		{slot.StatementID, slot.CustomerID, slot.FolioID, "issued", int64(640)},
		{slot.WithdrawalID, slot.JournalID, slot.StatementID, "pending", int64(640)},
		{slot.StatementID},
	}
	if got := billingArgs(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestBillingScenarioRequiresRuntimeSeedPlan(t *testing.T) {
	sess := &recordingBillingSession{}

	err := NewBillingJournalPostingBill().Run(context.Background(), sess, SeedState{})
	if err == nil {
		t.Fatal("Run() error = nil, want missing billing seed plan error")
	}
	if !strings.Contains(err.Error(), "billing seed plan required") {
		t.Fatalf("Run() error = %v, want missing billing seed plan error", err)
	}
	if got := len(sess.calls); got != 0 {
		t.Fatalf("len(calls) = %d, want 0", got)
	}
}

func billingMethods(calls []billingCall) []string {
	methods := make([]string, 0, len(calls))
	for _, call := range calls {
		methods = append(methods, call.method)
	}
	return methods
}

func billingQueries(calls []billingCall) []string {
	queries := make([]string, 0, len(calls))
	for _, call := range calls {
		queries = append(queries, call.query)
	}
	return queries
}

func billingArgs(calls []billingCall) [][]any {
	args := make([][]any, 0, len(calls))
	for _, call := range calls {
		args = append(args, call.args)
	}
	return args
}

func testBillingSeedState() SeedState {
	return SeedState{
		Billing: seed.BillingSeedPlan{
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
			StatementFolioParentUpdateProbeSlots: []seed.StatementFolioParentUpdateProbeSlot{
				{CustomerID: 1, FolioID: 1001, StatementID: 23001},
				{CustomerID: 2, FolioID: 1002, StatementID: 23002},
			},
		},
	}
}
