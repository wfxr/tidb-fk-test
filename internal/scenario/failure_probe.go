package scenario

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/db"
)

type paymentBillUpdateProbe struct {
	runCounter atomic.Uint64
}

func NewPaymentBillUpdateProbe() Scenario {
	return &paymentBillUpdateProbe{}
}

func (s *paymentBillUpdateProbe) Meta() Metadata {
	return Metadata{
		Name:               "payment_bill_update_probe",
		Group:              FailureProbeGroup,
		Weight:             1,
		ConcurrencyHint:    1,
		ExpectedErrorMatch: paymentBillUpdateProbeExpectedError,
	}
}

func (s *paymentBillUpdateProbe) Run(ctx context.Context, sess db.TxSession, seedState SeedState) error {
	if missingPropertyMeSeedPlan(seedState.PropertyMe) {
		return fmt.Errorf("payment_bill_update_probe: propertyme seed plan required")
	}

	run := s.runCounter.Add(1) - 1
	slot, err := selectPoolSlot(
		"payment_bill_update_probe",
		seedState.PropertyMe.PaymentBillUpdateProbeSlots,
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
