package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/wenxuan/dev/tidbcloud/upgrade-poc/internal/model"
)

func TestLogEventWritesStructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case slog.TimeKey, slog.LevelKey, slog.MessageKey:
				return slog.Attr{}
			default:
				return attr
			}
		},
	}))

	ts := time.Date(2026, time.May, 25, 14, 30, 0, 0, time.UTC)
	LogEvent(logger, Event{
		Timestamp:    ts,
		Phase:        "during-upgrade",
		Scenario:     "payment_bill_update_probe",
		WorkerID:     7,
		TxnName:      "payment_bill_update",
		StepName:     "commit",
		ClassifiedAs: model.ExpectedFKFailure,
		ErrorText:    "ERROR 1105 (HY000): upgrading a shared lock to an exclusive lock is not supported",
	})

	got := decodeLogLine(t, buf.Bytes())

	if got["timestamp"] != ts.Format(time.RFC3339) {
		t.Fatalf("timestamp = %v, want %q", got["timestamp"], ts.Format(time.RFC3339))
	}
	if got["phase"] != "during-upgrade" {
		t.Fatalf("phase = %v, want during-upgrade", got["phase"])
	}
	if got["scenario"] != "payment_bill_update_probe" {
		t.Fatalf("scenario = %v, want payment_bill_update_probe", got["scenario"])
	}
	if got["worker_id"] != float64(7) {
		t.Fatalf("worker_id = %v, want 7", got["worker_id"])
	}
	if got["txn_name"] != "payment_bill_update" {
		t.Fatalf("txn_name = %v, want payment_bill_update", got["txn_name"])
	}
	if got["step_name"] != "commit" {
		t.Fatalf("step_name = %v, want commit", got["step_name"])
	}
	if got["classified_as"] != string(model.ExpectedFKFailure) {
		t.Fatalf("classified_as = %v, want %q", got["classified_as"], model.ExpectedFKFailure)
	}
	if got["error_text"] != "ERROR 1105 (HY000): upgrading a shared lock to an exclusive lock is not supported" {
		t.Fatalf("error_text = %v, want expected FK error", got["error_text"])
	}
}

func TestLogEventOmitsEmptyErrorText(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case slog.TimeKey, slog.LevelKey, slog.MessageKey:
				return slog.Attr{}
			default:
				return attr
			}
		},
	}))

	LogEvent(logger, Event{
		Timestamp:    time.Date(2026, time.May, 25, 14, 31, 0, 0, time.UTC),
		Phase:        "pre-upgrade",
		Scenario:     "generic_update",
		WorkerID:     1,
		TxnName:      "generic_update",
		StepName:     "commit",
		ClassifiedAs: model.Success,
	})

	got := decodeLogLine(t, buf.Bytes())

	if _, ok := got["error_text"]; ok {
		t.Fatalf("error_text present in log record: %#v", got)
	}
}

func decodeLogLine(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, raw = %q", err, raw)
	}
	return got
}
