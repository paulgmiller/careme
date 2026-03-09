package kroger

import "testing"

func TestIdentityProviderSignature_UsesJSONStaples(t *testing.T) {
	got := NewIdentityProvider().Signature()
	want := mustJSONSignature(defaultStaples())

	if got != want {
		t.Fatalf("unexpected signature: got %q want %q", got, want)
	}
}
