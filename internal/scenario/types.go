package scenario

import (
	"context"
	"errors"
	"strings"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/db"
	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/seed"
)

type Group string

const (
	GenericGroup      Group = "generic_success"
	PropertyMeGroup   Group = "propertyme_success"
	FailureProbeGroup Group = "expected_failure_probe"
)

var ErrScenarioNotImplemented = errors.New("scenario not implemented")

type Metadata struct {
	Name               string `json:"name"`
	Group              Group  `json:"group"`
	Weight             int    `json:"weight"`
	ConcurrencyHint    int    `json:"concurrency_hint"`
	ExpectedErrorMatch string `json:"expected_error_match,omitempty"`
}

func (m Metadata) MatchesExpectedError(err error) bool {
	if err == nil || m.ExpectedErrorMatch == "" {
		return false
	}

	return strings.Contains(
		strings.ToLower(err.Error()),
		strings.ToLower(m.ExpectedErrorMatch),
	)
}

type SeedState struct {
	Generic seed.GenericSeedPlan
}

type Scenario interface {
	Meta() Metadata
	Run(ctx context.Context, sess db.TxSession, seed SeedState) error
}
