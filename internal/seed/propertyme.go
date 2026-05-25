package seed

import "github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"

const propertyMeFixturePoolSize = 4

type JournalPostingBillSlot struct {
	CustomerID int64
	FolioID    int64
	JournalID  int64
	PostingID  int64
	BillID     int64
}

type FolioBalanceUpdateSlot struct {
	CustomerID     int64
	FolioID        int64
	FolioBalanceID int64
}

type FKBackfillSlot struct {
	CustomerID   int64
	FolioID      int64
	JournalID    int64
	BillID       int64
	FeeLogID     int64
	StatementID  int64
	WithdrawalID int64
}

type PaymentMixedReferenceSlot struct {
	CustomerID int64
	FolioID    int64
	BillID     int64
	JournalID  int64
	PaymentID  int64
}

type CascadePathSlot struct {
	CustomerID   int64
	FolioID      int64
	JournalID    int64
	StatementID  int64
	WithdrawalID int64
}

type PaymentBillUpdateProbeSlot struct {
	CustomerID int64
	FolioID    int64
	BillID     int64
	JournalID  int64
	PaymentID  int64
}

type PropertyMeSeedPlan struct {
	CustomerIDs []int64
	FolioIDs    []int64

	ExistingCustomerID int64
	ExistingFolioID    int64
	ExistingJournalID  int64
	ExistingBillID     int64

	JournalPostingBillSlots     []JournalPostingBillSlot
	FolioBalanceUpdateSlots     []FolioBalanceUpdateSlot
	FKBackfillSlots             []FKBackfillSlot
	PaymentMixedReferenceSlots  []PaymentMixedReferenceSlot
	CascadePathSlots            []CascadePathSlot
	PaymentBillUpdateProbeSlots []PaymentBillUpdateProbeSlot
}

func PropertyMePlan(cfg config.Config) PropertyMeSeedPlan {
	customerCount := max(cfg.SeedParentRowsPerTable, 0)
	if customerCount == 0 {
		return PropertyMeSeedPlan{}
	}

	customerIDs := make([]int64, 0, customerCount)
	folioIDs := make([]int64, 0, customerCount)
	for i := 0; i < customerCount; i++ {
		customerIDs = append(customerIDs, int64(i+1))
		folioIDs = append(folioIDs, int64(1001+i))
	}

	plan := PropertyMeSeedPlan{
		CustomerIDs:        customerIDs,
		FolioIDs:           folioIDs,
		ExistingCustomerID: customerIDs[0],
		ExistingFolioID:    folioIDs[0],
		ExistingJournalID:  3001,
		ExistingBillID:     4001,
	}

	plan.JournalPostingBillSlots = buildJournalPostingBillSlots(customerIDs, folioIDs, 10001, 11001, 12001)
	plan.FolioBalanceUpdateSlots = buildFolioBalanceUpdateSlots(customerIDs, folioIDs, 13001)
	plan.FKBackfillSlots = buildFKBackfillSlots(customerIDs, folioIDs, 14001, 15001, 16001, 17001, 18001)
	plan.PaymentMixedReferenceSlots = buildPaymentMixedReferenceSlots(
		customerIDs,
		folioIDs,
		plan.ExistingBillID,
		19001,
		20001,
	)
	plan.CascadePathSlots = buildCascadePathSlots(customerIDs, folioIDs, plan.ExistingJournalID, 21001, 22001)
	plan.PaymentBillUpdateProbeSlots = buildPaymentBillUpdateProbeSlots(
		customerIDs,
		folioIDs,
		plan.ExistingBillID,
		plan.ExistingJournalID,
		18001,
	)

	return plan
}

func buildJournalPostingBillSlots(
	customerIDs, folioIDs []int64,
	firstJournalID, firstPostingID, firstBillID int64,
) []JournalPostingBillSlot {
	slots := make([]JournalPostingBillSlot, 0, propertyMeFixturePoolSize)
	for i := 0; i < propertyMeFixturePoolSize; i++ {
		slots = append(slots, JournalPostingBillSlot{
			CustomerID: customerIDs[i%len(customerIDs)],
			FolioID:    folioIDs[i%len(folioIDs)],
			JournalID:  firstJournalID + int64(i),
			PostingID:  firstPostingID + int64(i),
			BillID:     firstBillID + int64(i),
		})
	}
	return slots
}

func buildFolioBalanceUpdateSlots(customerIDs, folioIDs []int64, firstBalanceID int64) []FolioBalanceUpdateSlot {
	slots := make([]FolioBalanceUpdateSlot, 0, propertyMeFixturePoolSize)
	for i := 0; i < propertyMeFixturePoolSize; i++ {
		slots = append(slots, FolioBalanceUpdateSlot{
			CustomerID:     customerIDs[i%len(customerIDs)],
			FolioID:        folioIDs[i%len(folioIDs)],
			FolioBalanceID: firstBalanceID + int64(i),
		})
	}
	return slots
}

func buildFKBackfillSlots(
	customerIDs, folioIDs []int64,
	firstJournalID, firstBillID, firstFeeLogID, firstStatementID, firstWithdrawalID int64,
) []FKBackfillSlot {
	slots := make([]FKBackfillSlot, 0, propertyMeFixturePoolSize)
	for i := 0; i < propertyMeFixturePoolSize; i++ {
		slots = append(slots, FKBackfillSlot{
			CustomerID:   customerIDs[i%len(customerIDs)],
			FolioID:      folioIDs[i%len(folioIDs)],
			JournalID:    firstJournalID + int64(i),
			BillID:       firstBillID + int64(i),
			FeeLogID:     firstFeeLogID + int64(i),
			StatementID:  firstStatementID + int64(i),
			WithdrawalID: firstWithdrawalID + int64(i),
		})
	}
	return slots
}

func buildPaymentMixedReferenceSlots(
	customerIDs, folioIDs []int64,
	billID, firstJournalID, firstPaymentID int64,
) []PaymentMixedReferenceSlot {
	slots := make([]PaymentMixedReferenceSlot, 0, propertyMeFixturePoolSize)
	for i := 0; i < propertyMeFixturePoolSize; i++ {
		slots = append(slots, PaymentMixedReferenceSlot{
			CustomerID: customerIDs[i%len(customerIDs)],
			FolioID:    folioIDs[i%len(folioIDs)],
			BillID:     billID,
			JournalID:  firstJournalID + int64(i),
			PaymentID:  firstPaymentID + int64(i),
		})
	}
	return slots
}

func buildCascadePathSlots(
	customerIDs, folioIDs []int64,
	journalID, firstStatementID, firstWithdrawalID int64,
) []CascadePathSlot {
	slots := make([]CascadePathSlot, 0, propertyMeFixturePoolSize)
	for i := 0; i < propertyMeFixturePoolSize; i++ {
		slots = append(slots, CascadePathSlot{
			CustomerID:   customerIDs[i%len(customerIDs)],
			FolioID:      folioIDs[i%len(folioIDs)],
			JournalID:    journalID,
			StatementID:  firstStatementID + int64(i),
			WithdrawalID: firstWithdrawalID + int64(i),
		})
	}
	return slots
}

func buildPaymentBillUpdateProbeSlots(
	customerIDs, folioIDs []int64,
	billID, journalID, firstPaymentID int64,
) []PaymentBillUpdateProbeSlot {
	slots := make([]PaymentBillUpdateProbeSlot, 0, propertyMeFixturePoolSize)
	for i := 0; i < propertyMeFixturePoolSize; i++ {
		slots = append(slots, PaymentBillUpdateProbeSlot{
			CustomerID: customerIDs[i%len(customerIDs)],
			FolioID:    folioIDs[i%len(folioIDs)],
			BillID:     billID,
			JournalID:  journalID,
			PaymentID:  firstPaymentID + int64(i),
		})
	}
	return slots
}
