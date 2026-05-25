package scenario

import (
	"context"
	"sort"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/db"
)

const paymentBillUpdateProbeExpectedError = "upgrading a shared lock to an exclusive lock is not supported"

type Registry struct {
	items map[string]Scenario
}

func NewRegistry() *Registry {
	r := &Registry{items: make(map[string]Scenario)}

	r.add(genericPlaceholder("generic_insert_existing_parent"))
	r.add(genericPlaceholder("generic_insert_parent_then_child"))
	r.add(genericPlaceholder("generic_update_child_no_fk_change"))
	r.add(genericPlaceholder("generic_rebind_child_fk"))
	r.add(genericPlaceholder("generic_insert_parent_then_update_child_fk"))
	r.add(genericPlaceholder("generic_delete_parent_cascade"))
	r.add(genericPlaceholder("generic_concurrent_hot_parent_insert"))
	r.add(failureProbePlaceholder("payment_bill_update_probe", paymentBillUpdateProbeExpectedError))
	r.add(propertyMePlaceholder("pm_journal_posting_bill"))
	r.add(propertyMePlaceholder("pm_folio_balance_update"))
	r.add(propertyMePlaceholder("pm_fk_backfill"))
	r.add(propertyMePlaceholder("pm_payment_mixed_references"))
	r.add(propertyMePlaceholder("pm_cascade_path"))

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

func propertyMePlaceholder(name string) Scenario {
	return &placeholderScenario{
		meta: Metadata{
			Name:            name,
			Group:           PropertyMeGroup,
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
