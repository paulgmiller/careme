package types

import (
	"errors"
	"testing"
)

func TestIsDisabledBackendError(t *testing.T) {
	err := DisabledBackendError("HEB")

	if !IsDisabledBackendError(err) {
		t.Fatalf("expected disabled backend error to be recognized")
	}
	if !IsDisabledBackendError(errors.Join(errors.New("wrapped"), err)) {
		t.Fatalf("expected wrapped disabled backend error to be recognized")
	}
	if IsDisabledBackendError(errors.New("other")) {
		t.Fatalf("expected unrelated error to be ignored")
	}
}
