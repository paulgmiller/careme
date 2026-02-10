package users

import "testing"

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "trim and lower",
			input: " Alice@Example.COM ",
			want:  "alice@example.com",
		},
		{
			name:  "newline trimmed",
			input: "bob@example.com\n",
			want:  "bob@example.com",
		},
		{
			name:  "already normalized",
			input: "carol@example.com",
			want:  "carol@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeEmail(tt.input); got != tt.want {
				t.Fatalf("normalizeEmail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
