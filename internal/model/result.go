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

func Classify(_ string, err error) Result {
	if err == nil {
		return Result{Kind: Success}
	}

	message := err.Error()
	normalized := strings.ToLower(message)

	switch {
	case strings.Contains(normalized, "upgrading a shared lock to an exclusive lock is not supported"):
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
