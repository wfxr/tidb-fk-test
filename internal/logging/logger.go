package logging

import (
	"log/slog"
	"time"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/model"
)

type Event struct {
	Timestamp    time.Time        `json:"timestamp"`
	Phase        string           `json:"phase"`
	Scenario     string           `json:"scenario"`
	WorkerID     int              `json:"worker_id"`
	TxnName      string           `json:"txn_name"`
	StepName     string           `json:"step_name"`
	ClassifiedAs model.ResultKind `json:"classified_as"`
	ErrorText    string           `json:"error_text,omitempty"`
}

func LogEvent(logger *slog.Logger, event Event) {
	if logger == nil {
		return
	}

	logger.Info("workload_event", event.logArgs()...)
}

func (e Event) logArgs() []any {
	args := []any{
		slog.Time("timestamp", e.Timestamp),
		slog.String("phase", e.Phase),
		slog.String("scenario", e.Scenario),
		slog.Int("worker_id", e.WorkerID),
		slog.String("txn_name", e.TxnName),
		slog.String("step_name", e.StepName),
		slog.String("classified_as", string(e.ClassifiedAs)),
	}
	if e.ErrorText != "" {
		args = append(args, slog.String("error_text", e.ErrorText))
	}
	return args
}
