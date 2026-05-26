package scenario

import (
	"context"
	"sort"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/db"
)

const sharedLockUpgradeExpectedError = "upgrading a shared lock to an exclusive lock is not supported"

type Registry struct {
	items map[string]Scenario
}

func NewRegistry() *Registry {
	r := &Registry{items: make(map[string]Scenario)}

	r.add(NewGenericInsertExistingParent())
	r.add(NewGenericInsertParentThenChild())
	r.add(NewGenericUpdateChildNoFKChange())
	r.add(NewGenericRebindChildFK())
	r.add(NewGenericInsertParentThenUpdateChildFK())
	r.add(NewGenericDeleteParentCascade())
	r.add(NewGenericConcurrentHotParentInsert())
	r.add(NewPaymentBillUpdateProbe())
	r.add(NewStatementFolioParentUpdateProbe())
	r.add(NewBillingJournalPostingBill())
	r.add(NewBillingFolioBalanceUpdate())
	r.add(NewBillingFKBackfill())
	r.add(NewBillingPaymentMixedReferences())
	r.add(NewBillingCascadePath())

	return r
}

func (r *Registry) Get(name string) (Scenario, bool) {
	s, ok := r.items[name]
	return s, ok
}

func (r *Registry) All() []Scenario {
	all := make([]Scenario, 0, len(r.items))
	for _, s := range r.items {
		all = append(all, s)
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].Meta().Name < all[j].Meta().Name
	})

	return all
}

func (r *Registry) add(s Scenario) {
	meta := s.Meta()
	if meta.Name == "" {
		panic("scenario metadata name must not be empty")
	}
	if _, exists := r.items[meta.Name]; exists {
		panic("duplicate scenario registration: " + meta.Name)
	}

	r.items[meta.Name] = s
}

func genericPlaceholder(name string) Scenario {
	return &placeholderScenario{
		meta: Metadata{
			Name:            name,
			Group:           GenericGroup,
			Weight:          1,
			ConcurrencyHint: 1,
		},
	}
}

func billingPlaceholder(name string) Scenario {
	return &placeholderScenario{
		meta: Metadata{
			Name:            name,
			Group:           BillingGroup,
			Weight:          1,
			ConcurrencyHint: 1,
		},
	}
}

func failureProbePlaceholder(name, expectedError string) Scenario {
	return &placeholderScenario{
		meta: Metadata{
			Name:               name,
			Group:              FailureProbeGroup,
			Weight:             1,
			ConcurrencyHint:    1,
			ExpectedErrorMatch: expectedError,
		},
	}
}

type placeholderScenario struct {
	meta Metadata
}

func (s *placeholderScenario) Meta() Metadata {
	return s.meta
}

func (s *placeholderScenario) Run(context.Context, db.TxSession, SeedState) error {
	return ErrScenarioNotImplemented
}
