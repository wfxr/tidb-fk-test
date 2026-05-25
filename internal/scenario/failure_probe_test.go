package scenario

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

var _ Scenario = NewPaymentBillUpdateProbe()

func TestPaymentBillUpdateProbeMetadata(t *testing.T) {
	s := NewPaymentBillUpdateProbe()
	meta := s.Meta()

	if meta.Name != "payment_bill_update_probe" {
		t.Fatalf("Meta().Name = %q, want payment_bill_update_probe", meta.Name)
	}
	if meta.Group != FailureProbeGroup {
		t.Fatalf("Meta().Group = %q, want %q", meta.Group, FailureProbeGroup)
	}
	if meta.ExpectedErrorMatch == "" {
		t.Fatal("ExpectedErrorMatch should not be empty")
	}
}

func TestPaymentBillUpdateProbeOrder(t *testing.T) {
	sess := &recordingTxSession{}
	seedState := testPropertyMeSeedState()
	slot := seedState.PropertyMe.PaymentBillUpdateProbeSlots[0]

	err := NewPaymentBillUpdateProbe().Run(context.Background(), sess, seedState)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantQueries := []string{
		"DELETE FROM payment WHERE id = ?",
		"INSERT INTO payment (id, bill_id, journal_id, status, amount_cents) VALUES (?, ?, ?, ?, ?)",
		"UPDATE bill SET paid_cents = paid_cents + ?, version = version + 1 WHERE id = ?",
	}
	if got := queriesFromCalls(sess.calls); !reflect.DeepEqual(got, wantQueries) {
		t.Fatalf("queries = %v, want %v", got, wantQueries)
	}

	wantArgs := [][]any{
		{slot.PaymentID},
		{slot.PaymentID, slot.BillID, slot.JournalID, "pending", int64(450)},
		{int64(450), slot.BillID},
	}
	if got := argsFromCalls(sess.calls); !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("args = %v, want %v", got, wantArgs)
	}
}

func TestPaymentBillUpdateProbeRequiresRuntimeSeedPlan(t *testing.T) {
	sess := &recordingTxSession{}

	err := NewPaymentBillUpdateProbe().Run(context.Background(), sess, SeedState{})
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

func TestPaymentBillUpdateProbeStopsOnUpdateError(t *testing.T) {
	wantErr := errors.New("expected unsupported FK lock upgrade path")
	sess := &recordingTxSession{
		failAt: 3,
		err:    wantErr,
	}

	err := NewPaymentBillUpdateProbe().Run(context.Background(), sess, testPropertyMeSeedState())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error = %v, want %v", err, wantErr)
	}
	if got := len(sess.calls); got != 3 {
		t.Fatalf("len(calls) = %d, want 3", got)
	}
}
