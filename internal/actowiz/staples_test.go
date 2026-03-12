package actowiz

import (
	"testing"
)

func TestIdentityProviderSignature(t *testing.T) {
	t.Parallel()

	got := NewIdentityProvider().Signature()
	if got != "everything" {
		t.Fatalf("unexpected signature: got %q want everything", got)
	}
}

func TestStaplesProvider_FetchStaplesReturnsEmbeddedIngredients(t *testing.T) {
	t.Parallel()

	got, err := NewStaplesProvider().FetchStaples(t.Context(), "safeway_1234")
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected ingredients, got none")
	}

	first := got[0]
	if first.ProductId == nil || *first.ProductId == "" {
		t.Fatalf("unexpected product id: %+v", first.ProductId)
	}
	if first.Description == nil || *first.Description == "" {
		t.Fatalf("unexpected description: %+v", first.Description)
	}
	if first.Categories == nil || len(*first.Categories) == 0 {
		t.Fatalf("unexpected categories: %+v", first.Categories)
	}
}

func TestStaplesProvider_InvalidLocationID(t *testing.T) {
	t.Parallel()

	_, err := NewStaplesProvider().FetchStaples(t.Context(), "1234")
	if err == nil {
		t.Fatal("expected invalid location error")
	}
	if got, want := err.Error(), `invalid safeway location id "1234"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestStaplesProvider_GetIngredientsFiltersAndSkips(t *testing.T) {
	t.Parallel()

	got, err := NewStaplesProvider().GetIngredients(t.Context(), "safeway_1234", "salmon", 1)
	if err != nil {
		t.Fatalf("GetIngredients returned error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected filtered ingredients after skip")
	}
	if got[0].Description == nil {
		t.Fatalf("missing description: %+v", got[0])
	}
}
