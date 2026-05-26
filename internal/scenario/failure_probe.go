package scenario

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/db"
)

type paymentBillUpdateProbe struct {
	runCounter atomic.Uint64
}

type statementFolioParentUpdateProbe struct {
	runCounter atomic.Uint64
}

func NewPaymentBillUpdateProbe() Scenario {
	return &paymentBillUpdateProbe{}
}

func NewStatementFolioParentUpdateProbe() Scenario {
	return &statementFolioParentUpdateProbe{}
}

func (s *paymentBillUpdateProbe) Meta() Metadata {
	return Metadata{
		Name:               "payment_bill_update_probe",
		Group:              FailureProbeGroup,
		Weight:             1,
		ConcurrencyHint:    1,
		ExpectedErrorMatch: sharedLockUpgradeExpectedError,
	}
}

func (s *paymentBillUpdateProbe) Run(ctx context.Context, sess db.TxSession, seedState SeedState) error {
	if missingBillingSeedPlan(seedState.Billing) {
		return fmt.Errorf("payment_bill_update_probe: billing seed plan required")
	}

	run := s.runCounter.Add(1) - 1
	slot, err := selectPoolSlot(
		"payment_bill_update_probe",
		seedState.Billing.PaymentBillUpdateProbeSlots,
		run,
		"payment/bill update probe fixture slots",
	)
	if err != nil {
		return err
	}

	steps := []execStep{
		{query: "DELETE FROM payment WHERE id = ?", args: []any{slot.PaymentID}},
		{
			query: "INSERT INTO payment (id, bill_id, journal_id, status, amount_cents) VALUES (?, ?, ?, ?, ?)",
			args:  []any{slot.PaymentID, slot.BillID, slot.JournalID, "pending", int64(450)},
		},
		{
			query: "UPDATE bill SET paid_cents = paid_cents + ?, version = version + 1 WHERE id = ?",
			args:  []any{int64(450), slot.BillID},
		},
	}

	for _, step := range cloneExecSteps(steps) {
		if _, err := sess.ExecContext(ctx, step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *statementFolioParentUpdateProbe) Meta() Metadata {
	return Metadata{
		Name:               "statement_folio_parent_update_probe",
		Group:              FailureProbeGroup,
		Weight:             1,
		ConcurrencyHint:    1,
		ExpectedErrorMatch: sharedLockUpgradeExpectedError,
	}
}

func (s *statementFolioParentUpdateProbe) Run(ctx context.Context, sess db.TxSession, seedState SeedState) error {
	if missingBillingSeedPlan(seedState.Billing) {
		return fmt.Errorf("statement_folio_parent_update_probe: billing seed plan required")
	}

	run := s.runCounter.Add(1) - 1
	slot, err := selectPoolSlot(
		"statement_folio_parent_update_probe",
		seedState.Billing.StatementFolioParentUpdateProbeSlots,
		run,
		"statement/folio parent-update probe fixture slots",
	)
	if err != nil {
		return err
	}

	steps := []execStep{
		{query: "DELETE FROM statement WHERE id = ?", args: []any{slot.StatementID}},
		{
			query: "INSERT INTO statement (id, customer_id, folio_id, status, balance_cents) VALUES (?, ?, ?, ?, ?)",
			args:  []any{slot.StatementID, slot.CustomerID, slot.FolioID, "issued", int64(525)},
		},
		{
			query: "UPDATE folio SET last_statement_id = ? WHERE id = ?",
			args:  []any{slot.StatementID, slot.FolioID},
		},
	}

	for _, step := range cloneExecSteps(steps) {
		if _, err := sess.ExecContext(ctx, step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}
