package kroger

import "testing"

func TestClientWithResponsesIsID(t *testing.T) {
	t.Parallel()

	client := &ClientWithResponses{}
	tests := []struct {
		id   string
		want bool
	}{
		{id: "70500874", want: true},
		{id: "0001", want: true},
		{id: "", want: false},
		{id: "7050A874", want: false},
		{id: "walmart_123", want: false},
	}

	for _, tc := range tests {
		if got := client.IsID(tc.id); got != tc.want {
			t.Fatalf("IsID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestFloat32PtrToFloat64Ptr(t *testing.T) {
	t.Parallel()

	if got := float32PtrToFloat64Ptr(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}

	v := float32(47.5)
	got := float32PtrToFloat64Ptr(&v)
	if got == nil || *got != 47.5 {
		t.Fatalf("unexpected conversion: %v", got)
	}
}
