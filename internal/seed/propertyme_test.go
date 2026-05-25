package seed

import (
	"reflect"
	"testing"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/config"
)

func TestPropertyMePlanUsesDeterministicCoreIDsAndFixtures(t *testing.T) {
	plan := PropertyMePlan(config.Config{
		SeedParentRowsPerTable: 3,
	})

	wantCustomers := []int64{1, 2, 3}
	if !reflect.DeepEqual(plan.CustomerIDs, wantCustomers) {
		t.Fatalf("CustomerIDs = %v, want %v", plan.CustomerIDs, wantCustomers)
	}

	wantFolios := []int64{1001, 1002, 1003}
	if !reflect.DeepEqual(plan.FolioIDs, wantFolios) {
		t.Fatalf("FolioIDs = %v, want %v", plan.FolioIDs, wantFolios)
	}

	if got, want := plan.ExistingCustomerID, int64(1); got != want {
		t.Fatalf("ExistingCustomerID = %d, want %d", got, want)
	}
	if got, want := plan.ExistingFolioID, int64(1001); got != want {
		t.Fatalf("ExistingFolioID = %d, want %d", got, want)
	}
	if got, want := plan.ExistingJournalID, int64(3001); got != want {
		t.Fatalf("ExistingJournalID = %d, want %d", got, want)
	}
	if got, want := plan.ExistingBillID, int64(4001); got != want {
		t.Fatalf("ExistingBillID = %d, want %d", got, want)
	}

	firstChain := plan.JournalPostingBillSlots[0]
	if firstChain != (JournalPostingBillSlot{
		CustomerID: 1,
		FolioID:    1001,
		JournalID:  10001,
		PostingID:  11001,
		BillID:     12001,
	}) {
		t.Fatalf("first JournalPostingBillSlots entry = %+v", firstChain)
	}

	firstProbe := plan.PaymentBillUpdateProbeSlots[0]
	if firstProbe != (PaymentBillUpdateProbeSlot{
		PaymentID:  18001,
		BillID:     4001,
		JournalID:  3001,
		CustomerID: 1,
		FolioID:    1001,
	}) {
		t.Fatalf("first PaymentBillUpdateProbeSlots entry = %+v", firstProbe)
	}
}

func TestPropertyMePlanBuildsFixturePoolsForCustomerScenarios(t *testing.T) {
	plan := PropertyMePlan(config.Config{
		SeedParentRowsPerTable: 2,
	})

	if got, want := len(plan.JournalPostingBillSlots), propertyMeFixturePoolSize; got != want {
		t.Fatalf("len(JournalPostingBillSlots) = %d, want %d", got, want)
	}
	if got, want := len(plan.FolioBalanceUpdateSlots), propertyMeFixturePoolSize; got != want {
		t.Fatalf("len(FolioBalanceUpdateSlots) = %d, want %d", got, want)
	}
	if got, want := len(plan.FKBackfillSlots), propertyMeFixturePoolSize; got != want {
		t.Fatalf("len(FKBackfillSlots) = %d, want %d", got, want)
	}
	if got, want := len(plan.PaymentMixedReferenceSlots), propertyMeFixturePoolSize; got != want {
		t.Fatalf("len(PaymentMixedReferenceSlots) = %d, want %d", got, want)
	}
	if got, want := len(plan.CascadePathSlots), propertyMeFixturePoolSize; got != want {
		t.Fatalf("len(CascadePathSlots) = %d, want %d", got, want)
	}
	if got, want := len(plan.PaymentBillUpdateProbeSlots), propertyMeFixturePoolSize; got != want {
		t.Fatalf("len(PaymentBillUpdateProbeSlots) = %d, want %d", got, want)
	}
}

func TestPropertyMePlanDoesNotFabricateFixturesWithoutCustomers(t *testing.T) {
	plan := PropertyMePlan(config.Config{
		SeedParentRowsPerTable: 0,
	})

	if got := len(plan.CustomerIDs); got != 0 {
		t.Fatalf("len(CustomerIDs) = %d, want 0", got)
	}
	if got := len(plan.FolioIDs); got != 0 {
		t.Fatalf("len(FolioIDs) = %d, want 0", got)
	}
	if got := len(plan.JournalPostingBillSlots); got != 0 {
		t.Fatalf("len(JournalPostingBillSlots) = %d, want 0", got)
	}
	if got := len(plan.PaymentBillUpdateProbeSlots); got != 0 {
		t.Fatalf("len(PaymentBillUpdateProbeSlots) = %d, want 0", got)
	}
}
