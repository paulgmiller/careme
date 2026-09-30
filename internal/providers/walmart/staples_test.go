package walmart

import (
	"strings"
	"testing"
)

func TestStaplesProvider_FetchWines_ReturnsUnsupported(t *testing.T) {
	provider := NewStaplesProvider()

	_, err := provider.FetchWines(t.Context(), "walmart_3098", []string{"Pinot Noir"})
	if err == nil {
		t.Fatal("expected unsupported wine lookup error")
	}
	if !strings.Contains(err.Error(), `wine lookup is not supported for location "walmart_3098"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
