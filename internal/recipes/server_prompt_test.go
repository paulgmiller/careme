package recipes

import (
	"testing"
)

func TestMergeInstructions(t *testing.T) {
	t.Run("profile only", func(t *testing.T) {
		got := mergeInstructions("Always include one vegetarian meal.", "")
		want := "Always include one vegetarian meal."
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("request only", func(t *testing.T) {
		got := mergeInstructions("", "No shellfish")
		want := "No shellfish"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("profile and request", func(t *testing.T) {
		got := mergeInstructions("Always include one vegetarian meal.", "No shellfish")
		want := "Always include one vegetarian meal.\n\nNo shellfish"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})
}
