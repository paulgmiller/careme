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
		assert.Equal(t, "alpha", got["first"].Secrets["API_KEY"].Value)
		assert.Equal(t, "bravo", got["first"].Secrets["TOKEN"].Value)
		assert.Equal(t, "98101", got["second"].Secrets["ZIP"].Value)

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
		assert.Equal(t, "alpha", first.Secrets["API_KEY"].Value)
		assert.Equal(t, "# primary key", first.Secrets["API_KEY"].Comment)
		assert.Equal(t, "beta # still value", first.Secrets["TOKEN"].Value)
		assert.Equal(t, "# comment", first.Secrets["TOKEN"].Comment)
		assert.Equal(t, "with#hash", first.Secrets["PATH"].Value)
		assert.Empty(t, first.Secrets["PATH"].Comment)
	})

	t.Run("keeps block comments", func(t *testing.T) {
		t.Parallel()

		got, err := secrets(strings.NewReader(`
# top comment
#secret:first
# key note
API_KEY=alpha
# another note
TOKEN=bravo
`))
		require.NoError(t, err)
		require.Contains(t, got, "first")
		assert.Equal(t, []string{"# key note", "# another note"}, got["first"].Comments)
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

func TestParseSetArg(t *testing.T) {
	t.Parallel()

	secretName, key, value, err := parseSetArg("app/API_KEY=alpha")
	require.NoError(t, err)
	assert.Equal(t, "app", secretName)
	assert.Equal(t, "API_KEY", key)
	assert.Equal(t, "alpha", value)

	_, _, _, err = parseSetArg("app/API_KEY=no")
	require.Error(t, err)

	_, _, _, err = parseSetArg("API_KEY=alpha")
	require.Error(t, err)
}

func TestSetSecretValue(t *testing.T) {
	t.Parallel()

	t.Run("updates existing key and preserves comments", func(t *testing.T) {
		t.Parallel()

		input := []byte(`# top comment
#secret:first
# key note
API_KEY=alpha # primary key
TOKEN="beta # still value" # token note

# between
#secret:second
ZIP=98101
`)

		got, changed, err := setSecretValue(input, "first", "API_KEY", "bravo")
		require.NoError(t, err)
		require.True(t, changed)

		assert.Equal(t, `# top comment
#secret:first
# key note
API_KEY=bravo # primary key
TOKEN="beta # still value" # token note

# between
#secret:second
ZIP=98101
`, string(got))
	})

	t.Run("returns unchanged when value already matches", func(t *testing.T) {
		t.Parallel()

		input := []byte("#secret:first\nAPI_KEY=alpha # primary key\n")
		got, changed, err := setSecretValue(input, "first", "API_KEY", "alpha")
		require.NoError(t, err)
		require.False(t, changed)
		assert.Equal(t, input, got)
	})

	t.Run("adds key to existing secret without moving comments", func(t *testing.T) {
		t.Parallel()

		input := []byte(`#secret:first
API_KEY=alpha

# keep this with first
#secret:second
ZIP=98101
`)

		got, changed, err := setSecretValue(input, "first", "TOKEN", "bravo")
		require.NoError(t, err)
		require.True(t, changed)

		assert.Equal(t, `#secret:first
API_KEY=alpha

TOKEN=bravo
# keep this with first
#secret:second
ZIP=98101
`, string(got))
	})

	t.Run("adds new secret at end", func(t *testing.T) {
		t.Parallel()

		input := []byte("#secret:first\nAPI_KEY=alpha\n")
		got, changed, err := setSecretValue(input, "second", "TOKEN", "bravo")
		require.NoError(t, err)
		require.True(t, changed)

		assert.Equal(t, `#secret:first
API_KEY=alpha

#secret:second
TOKEN=bravo
`, string(got))
	})

	t.Run("quotes values with comments", func(t *testing.T) {
		t.Parallel()

		input := []byte("#secret:first\nAPI_KEY=alpha # primary key\n")
		got, changed, err := setSecretValue(input, "first", "API_KEY", "bravo # value")
		require.NoError(t, err)
		require.True(t, changed)

		assert.Equal(t, "#secret:first\nAPI_KEY=\"bravo # value\" # primary key\n", string(got))
	})
}
