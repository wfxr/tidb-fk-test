package report

import (
	"sort"
	"sync"
	"time"

	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/model"
	"github.com/wenxuan/dev/tidbcloud/tidb-fk-test/internal/scenario"
)

type Totals struct {
	Executed          int64 `json:"executed"`
	Success           int64 `json:"success"`
	ExpectedFailure   int64 `json:"expected_failure"`
	UnexpectedFailure int64 `json:"unexpected_failure"`
}

type ScenarioSummary struct {
	Name              string    `json:"name"`
	Executed          int64     `json:"executed"`
	Success           int64     `json:"success"`
	ExpectedFailure   int64     `json:"expected_failure"`
	UnexpectedFailure int64     `json:"unexpected_failure"`
	LastErrorAt       time.Time `json:"last_error_at,omitempty"`
	LastErrorText     string    `json:"last_error_text,omitempty"`
}

type Summary struct {
	mu        sync.RWMutex
	totals    Totals
	scenarios map[string]ScenarioSummary
}

func NewSummary(items []scenario.Scenario) *Summary {
	summaries := make(map[string]ScenarioSummary, len(items))
	for _, item := range items {
		meta := item.Meta()
		summaries[meta.Name] = ScenarioSummary{Name: meta.Name}
	}

	return &Summary{
		scenarios: summaries,
	}
}

func (s *Summary) Record(meta scenario.Metadata, result model.Result, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := s.scenarios[meta.Name]
	if stats.Name == "" {
		stats.Name = meta.Name
	}

	stats.Executed++
	s.totals.Executed++

	switch result.Kind {
	case model.Success:
		stats.Success++
		s.totals.Success++
	case model.ExpectedFKFailure:
		stats.ExpectedFailure++
		s.totals.ExpectedFailure++
	default:
		stats.UnexpectedFailure++
		s.totals.UnexpectedFailure++
	}

	if result.ErrorText != "" {
		stats.LastErrorAt = at
		stats.LastErrorText = result.ErrorText
	}

	s.scenarios[meta.Name] = stats
}

func (s *Summary) RecordClassified(meta scenario.Metadata, err error, at time.Time) model.Result {
	result := model.Classify(meta.ExpectedErrorMatch, err)
	s.Record(meta, result, at)
	return result
}

func (s *Summary) Totals() Totals {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.totals
}

func (s *Summary) Scenario(name string) (ScenarioSummary, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats, ok := s.scenarios[name]
	return stats, ok
}

func (s *Summary) Scenarios() []ScenarioSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]ScenarioSummary, 0, len(s.scenarios))
	for _, stats := range s.scenarios {
		items = append(items, stats)
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})

	return items
}
