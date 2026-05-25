package model

import "strings"

type ResultKind string

const (
	Success                 ResultKind = "success"
	ExpectedFKFailure       ResultKind = "expected_fk_failure"
	InfraOrUpgradeTransient ResultKind = "infra_or_upgrade_transient"
	UnexpectedFailure       ResultKind = "unexpected_failure"
)

type Result struct {
	Kind      ResultKind `json:"kind"`
	ErrorText string     `json:"error_text,omitempty"`
}

func Classify(expectedErrorMatch string, err error) Result {
	if err == nil {
		return Result{Kind: Success}
	}

	message := err.Error()
	normalized := strings.ToLower(message)
	expected := strings.ToLower(expectedErrorMatch)

	switch {
	case expected != "" && strings.Contains(normalized, expected):
		return Result{Kind: ExpectedFKFailure, ErrorText: message}
	case strings.Contains(normalized, "driver: bad connection"),
		strings.Contains(normalized, "context deadline exceeded"),
		strings.Contains(normalized, "connection refused"),
		strings.Contains(normalized, "i/o timeout"):
		return Result{Kind: InfraOrUpgradeTransient, ErrorText: message}
	default:
		return Result{Kind: UnexpectedFailure, ErrorText: message}
	}
}
