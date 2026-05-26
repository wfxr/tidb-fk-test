package model

import (
	"errors"
	"testing"
)

func TestClassifySuccess(t *testing.T) {
	res := Classify("generic_update", nil)

	if res.Kind != Success {
		t.Fatalf("Kind = %q, want %q", res.Kind, Success)
	}
	if res.ErrorText != "" {
		t.Fatalf("ErrorText = %q, want empty", res.ErrorText)
	}
}

func TestClassifyExpectedFKFailure(t *testing.T) {
	res := Classify("upgrading a shared lock to an exclusive lock is not supported", errors.New(
		"ERROR 1105 (HY000): upgrading a shared lock to an exclusive lock is not supported",
	))

	if res.Kind != ExpectedFKFailure {
		t.Fatalf("Kind = %q, want %q", res.Kind, ExpectedFKFailure)
	}
}

func TestClassifySharedLockUpgradeErrorWithoutExpectedMatchIsUnexpected(t *testing.T) {
	res := Classify("", errors.New(
		"ERROR 1105 (HY000): upgrading a shared lock to an exclusive lock is not supported",
	))

	if res.Kind != UnexpectedFailure {
		t.Fatalf("Kind = %q, want %q", res.Kind, UnexpectedFailure)
	}
}

func TestClassifyInfraOrUpgradeTransient(t *testing.T) {
	testCases := []struct {
		name string
		err  error
	}{
		{
			name: "bad connection",
			err:  errors.New("driver: bad connection"),
		},
		{
			name: "deadline exceeded",
			err:  errors.New("context deadline exceeded"),
		},
		{
			name: "connection refused",
			err:  errors.New("dial tcp 127.0.0.1:4000: connect: connection refused"),
		},
		{
			name: "i/o timeout",
			err:  errors.New("dial tcp 127.0.0.1:4000: i/o timeout"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res := Classify("billing_invoice_update", tc.err)

			if res.Kind != InfraOrUpgradeTransient {
				t.Fatalf("Kind = %q, want %q", res.Kind, InfraOrUpgradeTransient)
			}
			if res.ErrorText != tc.err.Error() {
				t.Fatalf("ErrorText = %q, want %q", res.ErrorText, tc.err.Error())
			}
		})
	}
}

func TestClassifyUnexpectedFailure(t *testing.T) {
	err := errors.New("Error 1452 (23000): Cannot add or update a child row")

	res := Classify("billing_payment_insert", err)

	if res.Kind != UnexpectedFailure {
		t.Fatalf("Kind = %q, want %q", res.Kind, UnexpectedFailure)
	}
	if res.ErrorText != err.Error() {
		t.Fatalf("ErrorText = %q, want %q", res.ErrorText, err.Error())
	}
}
