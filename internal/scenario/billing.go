package scenario

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/db"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/seed"
)

type billingStep struct {
	query    string
	args     []any
	queryRow bool
}

type billingExecScenario struct {
	meta       Metadata
	runCounter atomic.Uint64
	buildSteps func(plan seed.BillingSeedPlan, run uint64) ([]billingStep, error)
}

func NewBillingJournalPostingBill() Scenario {
	return newBillingExecScenario(
		"billing_journal_posting_bill",
		2,
		func(plan seed.BillingSeedPlan, run uint64) ([]billingStep, error) {
			slot, err := selectPoolSlot(
				"billing_journal_posting_bill",
				plan.JournalPostingBillSlots,
				run,
				"journal/posting/bill fixture slots",
			)
			if err != nil {
				return nil, err
			}

			return []billingStep{
				{query: "DELETE FROM bill WHERE id = ?", args: []any{slot.BillID}},
				{query: "DELETE FROM posting WHERE id = ?", args: []any{slot.PostingID}},
				{query: "DELETE FROM journal WHERE id = ?", args: []any{slot.JournalID}},
				{
					query: "INSERT INTO journal (id, customer_id, folio_id, member_id, reference, amount_cents) VALUES (?, ?, ?, ?, ?, ?)",
					args:  []any{slot.JournalID, slot.CustomerID, slot.FolioID, nil, "billing-journal", int64(1500)},
				},
				{
					query: "INSERT INTO posting (id, journal_id, status, amount_cents) VALUES (?, ?, ?, ?)",
					args:  []any{slot.PostingID, slot.JournalID, "posted", int64(1500)},
				},
				{
					query: "INSERT INTO bill (id, journal_id, folio_id, status, total_cents, paid_cents) VALUES (?, ?, ?, ?, ?, ?)",
					args:  []any{slot.BillID, slot.JournalID, slot.FolioID, "open", int64(1500), int64(0)},
				},
			}, nil
		},
	)
}

func NewBillingFolioBalanceUpdate() Scenario {
	return newBillingExecScenario(
		"billing_folio_balance_update",
		2,
		func(plan seed.BillingSeedPlan, run uint64) ([]billingStep, error) {
			slot, err := selectPoolSlot(
				"billing_folio_balance_update",
				plan.FolioBalanceUpdateSlots,
				run,
				"folio balance fixture slots",
			)
			if err != nil {
				return nil, err
			}

			return []billingStep{
				{
					query:    "SELECT id FROM foliobalance WHERE id = ? FOR UPDATE",
					args:     []any{slot.FolioBalanceID},
					queryRow: true,
				},
				{
					query: "UPDATE foliobalance SET balance_cents = balance_cents + ?, updated_count = updated_count + 1 WHERE id = ?",
					args:  []any{int64(250), slot.FolioBalanceID},
				},
			}, nil
		},
	)
}

func NewBillingFKBackfill() Scenario {
	return newBillingExecScenario(
		"billing_fk_backfill",
		1,
		func(plan seed.BillingSeedPlan, run uint64) ([]billingStep, error) {
			slot, err := selectPoolSlot(
				"billing_fk_backfill",
				plan.FKBackfillSlots,
				run,
				"FK backfill fixture slots",
			)
			if err != nil {
				return nil, err
			}

			return []billingStep{
				{query: "DELETE FROM feelog WHERE id = ?", args: []any{slot.FeeLogID}},
				{query: "DELETE FROM withdrawal WHERE id = ?", args: []any{slot.WithdrawalID}},
				{query: "DELETE FROM bill WHERE id = ?", args: []any{slot.BillID}},
				{query: "DELETE FROM statement WHERE id = ?", args: []any{slot.StatementID}},
				{
					query: "INSERT INTO feelog (id, journal_id, fee_bill_id, amount_cents) VALUES (?, ?, ?, ?)",
					args:  []any{slot.FeeLogID, slot.JournalID, nil, int64(225)},
				},
				{
					query: "INSERT INTO withdrawal (id, journal_id, statement_id, status, amount_cents) VALUES (?, ?, ?, ?, ?)",
					args:  []any{slot.WithdrawalID, slot.JournalID, nil, "pending", int64(300)},
				},
				{
					query: "INSERT INTO bill (id, journal_id, folio_id, status, total_cents, paid_cents) VALUES (?, ?, ?, ?, ?, ?)",
					args:  []any{slot.BillID, slot.JournalID, slot.FolioID, "open", int64(225), int64(0)},
				},
				{
					query: "UPDATE feelog SET fee_bill_id = ? WHERE id = ?",
					args:  []any{slot.BillID, slot.FeeLogID},
				},
				{
					query: "INSERT INTO statement (id, customer_id, folio_id, status, balance_cents) VALUES (?, ?, ?, ?, ?)",
					args:  []any{slot.StatementID, slot.CustomerID, slot.FolioID, "issued", int64(525)},
				},
				{
					query: "UPDATE withdrawal SET statement_id = ? WHERE id = ?",
					args:  []any{slot.StatementID, slot.WithdrawalID},
				},
			}, nil
		},
	)
}

func NewBillingPaymentMixedReferences() Scenario {
	return newBillingExecScenario(
		"billing_payment_mixed_references",
		2,
		func(plan seed.BillingSeedPlan, run uint64) ([]billingStep, error) {
			slot, err := selectPoolSlot(
				"billing_payment_mixed_references",
				plan.PaymentMixedReferenceSlots,
				run,
				"payment mixed-reference fixture slots",
			)
			if err != nil {
				return nil, err
			}

			return []billingStep{
				{query: "DELETE FROM payment WHERE id = ?", args: []any{slot.PaymentID}},
				{query: "DELETE FROM journal WHERE id = ?", args: []any{slot.JournalID}},
				{
					query: "INSERT INTO journal (id, customer_id, folio_id, member_id, reference, amount_cents) VALUES (?, ?, ?, ?, ?, ?)",
					args:  []any{slot.JournalID, slot.CustomerID, slot.FolioID, nil, "billing-payment", int64(875)},
				},
				{
					query: "INSERT INTO payment (id, bill_id, journal_id, status, amount_cents) VALUES (?, ?, ?, ?, ?)",
					args:  []any{slot.PaymentID, slot.BillID, slot.JournalID, "applied", int64(875)},
				},
			}, nil
		},
	)
}

func NewBillingCascadePath() Scenario {
	return newBillingExecScenario(
		"billing_cascade_path",
		1,
		func(plan seed.BillingSeedPlan, run uint64) ([]billingStep, error) {
			slot, err := selectPoolSlot(
				"billing_cascade_path",
				plan.CascadePathSlots,
				run,
				"cascade fixture slots",
			)
			if err != nil {
				return nil, err
			}

			return []billingStep{
				{query: "DELETE FROM withdrawal WHERE id = ?", args: []any{slot.WithdrawalID}},
				{query: "DELETE FROM statement WHERE id = ?", args: []any{slot.StatementID}},
				{
					query: "INSERT INTO statement (id, customer_id, folio_id, status, balance_cents) VALUES (?, ?, ?, ?, ?)",
					args:  []any{slot.StatementID, slot.CustomerID, slot.FolioID, "issued", int64(640)},
				},
				{
					query: "INSERT INTO withdrawal (id, journal_id, statement_id, status, amount_cents) VALUES (?, ?, ?, ?, ?)",
					args:  []any{slot.WithdrawalID, slot.JournalID, slot.StatementID, "pending", int64(640)},
				},
				{query: "DELETE FROM statement WHERE id = ?", args: []any{slot.StatementID}},
			}, nil
		},
	)
}

func newBillingExecScenario(
	name string,
	concurrencyHint int,
	buildSteps func(plan seed.BillingSeedPlan, run uint64) ([]billingStep, error),
) Scenario {
	return &billingExecScenario{
		meta: Metadata{
			Name:            name,
			Group:           BillingGroup,
			Weight:          1,
			ConcurrencyHint: concurrencyHint,
		},
		buildSteps: buildSteps,
	}
}

func (s *billingExecScenario) Meta() Metadata {
	return s.meta
}

func (s *billingExecScenario) Run(ctx context.Context, sess db.TxSession, seedState SeedState) error {
	if missingBillingSeedPlan(seedState.Billing) {
		return fmt.Errorf("%s: billing seed plan required", s.meta.Name)
	}

	run := s.runCounter.Add(1) - 1
	steps, err := s.buildSteps(seedState.Billing, run)
	if err != nil {
		return err
	}

	for _, step := range cloneBillingSteps(steps) {
		if step.queryRow {
			var lockedID int64
			if err := sess.QueryRowContext(ctx, step.query, step.args...).Scan(&lockedID); err != nil {
				return err
			}
			continue
		}
		if _, err := sess.ExecContext(ctx, step.query, step.args...); err != nil {
			return err
		}
	}
	return nil
}

func cloneBillingSteps(steps []billingStep) []billingStep {
	cloned := make([]billingStep, 0, len(steps))
	for _, step := range steps {
		cloned = append(cloned, billingStep{
			query:    step.query,
			args:     append([]any(nil), step.args...),
			queryRow: step.queryRow,
		})
	}
	return cloned
}

func missingBillingSeedPlan(plan seed.BillingSeedPlan) bool {
	return len(plan.CustomerIDs) == 0 &&
		len(plan.FolioIDs) == 0 &&
		len(plan.JournalPostingBillSlots) == 0 &&
		len(plan.FolioBalanceUpdateSlots) == 0 &&
		len(plan.FKBackfillSlots) == 0 &&
		len(plan.PaymentMixedReferenceSlots) == 0 &&
		len(plan.CascadePathSlots) == 0 &&
		len(plan.PaymentBillUpdateProbeSlots) == 0 &&
		len(plan.StatementFolioParentUpdateProbeSlots) == 0
}
