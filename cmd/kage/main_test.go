package main

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSecretNeedsUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current *corev1.Secret
		desired *corev1.Secret
		want    bool
	}{
		{
			name: "unchanged",
			current: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Annotations: map[string]string{
						managedByAnnotationKey: managedByAnnotationValue,
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"API_KEY": []byte("same"),
				},
			},
			desired: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Annotations: map[string]string{
						managedByAnnotationKey: managedByAnnotationValue,
					},
				},
				Type: corev1.SecretTypeOpaque,
				StringData: map[string]string{
					"API_KEY": "same",
				},
			},
			want: false,
		},
		{
			name: "changed value",
			current: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Annotations: map[string]string{
						managedByAnnotationKey: managedByAnnotationValue,
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"API_KEY": []byte("before"),
				},
			},
			desired: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Annotations: map[string]string{
						managedByAnnotationKey: managedByAnnotationValue,
					},
				},
				Type: corev1.SecretTypeOpaque,
				StringData: map[string]string{
					"API_KEY": "after",
				},
			},
			want: true,
		},
		{
			name: "removed key",
			current: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Annotations: map[string]string{
						managedByAnnotationKey: managedByAnnotationValue,
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"API_KEY": []byte("same"),
					"EXTRA":   []byte("remove"),
				},
			},
			desired: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Annotations: map[string]string{
						managedByAnnotationKey: managedByAnnotationValue,
					},
				},
				Type: corev1.SecretTypeOpaque,
				StringData: map[string]string{
					"API_KEY": "same",
				},
			},
			want: true,
		},
		{
			name: "added key",
			current: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Annotations: map[string]string{
						managedByAnnotationKey: managedByAnnotationValue,
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"API_KEY": []byte("same"),
				},
			},
			desired: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Annotations: map[string]string{
						managedByAnnotationKey: managedByAnnotationValue,
					},
				},
				Type: corev1.SecretTypeOpaque,
				StringData: map[string]string{
					"API_KEY": "same",
					"EXTRA":   "add",
				},
			},
			want: true,
		},
		{
			name: "annotation changed",
			current: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Annotations: map[string]string{
						managedByAnnotationKey: "someone-else",
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"API_KEY": []byte("same"),
				},
			},
			desired: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Annotations: map[string]string{
						managedByAnnotationKey: managedByAnnotationValue,
					},
				},
				Type: corev1.SecretTypeOpaque,
				StringData: map[string]string{
					"API_KEY": "same",
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := secretNeedsUpdate(tt.current, tt.desired)
			if got != tt.want {
				t.Fatalf("secretNeedsUpdate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSecrets(t *testing.T) {
	t.Parallel()

	t.Run("parses multiple secret blocks", func(t *testing.T) {
		t.Parallel()

		input := strings.NewReader(`
#secret:first
API_KEY=alpha
TOKEN=beta

#secret:second
ZIP=98101
`)

		got, err := secrets(input)
		if err != nil {
			t.Fatalf("secrets() error = %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(secrets()) = %d, want 2", len(got))
		}

		byName := map[string]*corev1.Secret{}
		for _, secret := range got {
			byName[secret.Name] = secret
		}

		first := byName["first"]
		if first == nil {
			t.Fatalf("missing secret %q", "first")
		}
		if first.Type != corev1.SecretTypeOpaque {
			t.Fatalf("first.Type = %q, want %q", first.Type, corev1.SecretTypeOpaque)
		}
		if first.Annotations[managedByAnnotationKey] != managedByAnnotationValue {
			t.Fatalf("first managed-by = %q", first.Annotations[managedByAnnotationKey])
		}
		if first.StringData["API_KEY"] != "alpha" {
			t.Fatalf("first API_KEY = %q, want %q", first.StringData["API_KEY"], "alpha")
		}
		if first.StringData["TOKEN"] != "beta" {
			t.Fatalf("first TOKEN = %q, want %q", first.StringData["TOKEN"], "beta")
		}

		second := byName["second"]
		if second == nil {
			t.Fatalf("missing secret %q", "second")
		}
		if second.StringData["ZIP"] != "98101" {
			t.Fatalf("second ZIP = %q, want %q", second.StringData["ZIP"], "98101")
		}
	})

	t.Run("handles end of line comments", func(t *testing.T) {
		t.Parallel()

		got, err := secrets(strings.NewReader(`
#secret:first
API_KEY=alpha # primary key
TOKEN="beta # still value" # comment
PATH=with#hash
`))
		if err != nil {
			t.Fatalf("secrets() error = %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len(secrets()) = %d, want 1", len(got))
		}

		first := got[0]
		if first.StringData["API_KEY"] != "alpha" {
			t.Fatalf("API_KEY = %q, want %q", first.StringData["API_KEY"], "alpha")
		}
		if first.StringData["TOKEN"] != "beta # still value" {
			t.Fatalf("TOKEN = %q, want %q", first.StringData["TOKEN"], "beta # still value")
		}
		if first.StringData["PATH"] != "with#hash" {
			t.Fatalf("PATH = %q, want %q", first.StringData["PATH"], "with#hash")
		}
	})

	t.Run("duplicate secret comment returns error", func(t *testing.T) {
		t.Parallel()

		_, err := secrets(strings.NewReader(`
#secret:first
API_KEY=alpha
#secret:first
TOKEN=beta
`))
		if err == nil {
			t.Fatal("secrets() error = nil, want duplicate secret comment error")
		}
	})

	t.Run("duplicate key returns error", func(t *testing.T) {
		t.Parallel()

		_, err := secrets(strings.NewReader(`
#secret:first
API_KEY=alpha
API_KEY=beta
`))
		if err == nil {
			t.Fatal("secrets() error = nil, want duplicate secret key error")
		}
	})

	t.Run("invalid entry returns error", func(t *testing.T) {
		t.Parallel()

		_, err := secrets(strings.NewReader(`
#secret:first
not-an-env-line
`))
		if err == nil {
			t.Fatal("secrets() error = nil, want invalid secret entry error")
		}
	})
}
