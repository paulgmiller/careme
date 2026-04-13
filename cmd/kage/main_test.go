package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			assert.Equal(t, tt.want, secretNeedsUpdate(tt.current, tt.desired))
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
TOKEN=bravo

#secret:second
ZIP=98101
		`)

		got, err := secrets(input)
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.Contains(t, got, "first")
		require.Contains(t, got, "second")
		assert.Equal(t, "alpha", got["first"]["API_KEY"])
		assert.Equal(t, "bravo", got["first"]["TOKEN"])
		assert.Equal(t, "98101", got["second"]["ZIP"])

		secretsK8s := toK8s(got)
		byName := map[string]*corev1.Secret{}
		for _, secret := range secretsK8s {
			byName[secret.Name] = secret
		}

		require.Contains(t, byName, "first")
		first := byName["first"]
		require.NotNil(t, first)
		assert.Equal(t, corev1.SecretTypeOpaque, first.Type)
		assert.Equal(t, managedByAnnotationValue, first.Annotations[managedByAnnotationKey])
		assert.Equal(t, "alpha", first.StringData["API_KEY"])
		assert.Equal(t, "bravo", first.StringData["TOKEN"])

		require.Contains(t, byName, "second")
		second := byName["second"]
		require.NotNil(t, second)
		assert.Equal(t, "98101", second.StringData["ZIP"])
	})

	t.Run("handles end of line comments", func(t *testing.T) {
		t.Parallel()

		got, err := secrets(strings.NewReader(`
#secret:first
API_KEY=alpha # primary key
TOKEN="beta # still value" # comment
PATH=with#hash
`))
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Contains(t, got, "first")

		first := got["first"]
		assert.Equal(t, "alpha", first["API_KEY"])
		assert.Equal(t, "beta # still value", first["TOKEN"])
		assert.Equal(t, "with#hash", first["PATH"])
	})

	t.Run("rejects short values", func(t *testing.T) {
		t.Parallel()

		_, err := secrets(strings.NewReader(`
#secret:first
EMPTY=
NON_EMPTY=value
`))
		require.Error(t, err)
	})

	t.Run("duplicate secret comment returns error", func(t *testing.T) {
		t.Parallel()

		_, err := secrets(strings.NewReader(`
#secret:first
API_KEY=alpha
#secret:first
TOKEN=beta
`))
		require.Error(t, err)
	})

	t.Run("duplicate key returns error", func(t *testing.T) {
		t.Parallel()

		_, err := secrets(strings.NewReader(`
#secret:first
API_KEY=alpha
API_KEY=bravo
`))
		require.Error(t, err)
	})

	t.Run("invalid entry returns error", func(t *testing.T) {
		t.Parallel()

		_, err := secrets(strings.NewReader(`
#secret:first
not-an-env-line
`))
		require.Error(t, err)
	})
}

func TestMaskedSecretValue(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a[5]a", maskedSecretValue("alpha"))
}
