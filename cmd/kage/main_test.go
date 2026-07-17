package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestLoadRecipients(t *testing.T) {
	t.Parallel()

	ageIdentity, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	_, sshPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPublicKey, err := ssh.NewPublicKey(sshPrivateKey.Public())
	require.NoError(t, err)
	sshIdentity, err := agessh.NewEd25519Identity(sshPrivateKey)
	require.NoError(t, err)

	directory := t.TempDir()
	recipientsPath := filepath.Join(directory, recipientsFilename)
	recipientsFile := strings.Join([]string{
		"# team keys",
		ageIdentity.Recipient().String(),
		strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPublicKey))),
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(recipientsPath, []byte(recipientsFile), 0o600))

	recipients, err := loadRecipients(recipientsPath)
	require.NoError(t, err)
	require.Len(t, recipients, 2)

	const plaintext = "secret contents"
	encryptedPath := filepath.Join(directory, "envtest")
	require.NoError(t, encryptFile(encryptedPath, recipients, func(writer io.Writer) error {
		_, err := io.WriteString(writer, plaintext)
		return err
	}))
	assert.Equal(t, plaintext, decryptFile(t, encryptedPath, ageIdentity))
	assert.Equal(t, plaintext, decryptFile(t, encryptedPath, sshIdentity))
}

func TestLoadRecipientsRejectsEmptyFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), recipientsFilename)
	require.NoError(t, os.WriteFile(path, []byte("# no keys\n"), 0o600))

	_, err := loadRecipients(path)
	require.ErrorContains(t, err, "no recipients")
}

func TestEncryptFileDoesNotReplaceFileOnFailure(t *testing.T) {
	t.Parallel()

	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "envtest")
	require.NoError(t, os.WriteFile(path, []byte("original ciphertext"), 0o600))

	err = encryptFile(path, []age.Recipient{identity.Recipient()}, func(writer io.Writer) error {
		_, writeErr := io.WriteString(writer, "partial plaintext")
		require.NoError(t, writeErr)
		return errors.New("write failed")
	})
	require.ErrorContains(t, err, "write failed")
	contents, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "original ciphertext", string(contents))
}

func decryptFile(t *testing.T, path string, identity age.Identity) string {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, file.Close())
	})
	reader, err := age.Decrypt(file, identity)
	require.NoError(t, err)
	plaintext, err := io.ReadAll(reader)
	require.NoError(t, err)
	return string(plaintext)
}

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
		assert.Equal(t, "first", got[0].Name)
		assert.Equal(t, "second", got[1].Name)
		assert.Equal(t, "alpha", got[0].Lines[0].Value)
		assert.Equal(t, "bravo", got[0].Lines[1].Value)
		assert.Equal(t, "98101", got[1].Lines[0].Value)

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
		assert.Equal(t, "first", got[0].Name)

		first := got[0]
		require.Len(t, first.Lines, 3)
		assert.Equal(t, secretLine{Key: "API_KEY", Value: "alpha", Comment: " primary key"}, first.Lines[0])
		assert.Equal(t, secretLine{Key: "TOKEN", Value: "beta # still value", Comment: " comment"}, first.Lines[1])
		assert.Equal(t, secretLine{Key: "PATH", Value: "with#hash"}, first.Lines[2])
	})

	t.Run("unquotes single quoted values before converting to k8s secrets", func(t *testing.T) {
		t.Parallel()

		const endpoint = "wss://user:pass@brd.superproxy.io:9222"
		got, err := secrets(strings.NewReader(`
#secret:brightdata-proxy
BRIGHTDATA_BROWSER_WS_ENDPOINT='` + endpoint + `'
		`))
		require.NoError(t, err)

		secretsK8s := toK8s(got)
		require.Len(t, secretsK8s, 1)
		assert.Equal(t, endpoint, secretsK8s[0].StringData["BRIGHTDATA_BROWSER_WS_ENDPOINT"])
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
		require.Len(t, got, 1)
		assert.Equal(t, "first", got[0].Name)
		assert.Len(t, got[0].Lines, 4)
		assert.Equal(t, []secretLine{
			{Comment: " key note"},
			{Key: "API_KEY", Value: "alpha"},
			{Comment: " another note"},
			{Key: "TOKEN", Value: "bravo"},
		}, got[0].Lines)
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

	t.Run("updates existing key and serializes parsed comments", func(t *testing.T) {
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
		ogFile, err := secrets(bytes.NewReader(input))
		require.NoError(t, err)
		got, changed := setSecretValue(ogFile, "first", "API_KEY", "bravo")
		require.True(t, changed)
		var sb strings.Builder
		require.NoError(t, got.write(&sb))
		assert.Equal(t, `#secret:first
# key note
API_KEY=bravo # primary key
TOKEN="beta # still value" # token note
# between

#secret:second
ZIP=98101
`, sb.String())
	})

	t.Run("returns unchanged when value already matches", func(t *testing.T) {
		t.Parallel()

		input := []byte("#secret:first\nAPI_KEY=alpha # primary key\n")
		ogFile, err := secrets(bytes.NewReader(input))
		require.NoError(t, err)
		_, changed := setSecretValue(ogFile, "first", "API_KEY", "alpha")

		require.False(t, changed)
	})

	t.Run("adds key to existing secret", func(t *testing.T) {
		t.Parallel()

		input := []byte(`#secret:first
API_KEY=alpha

# keep this with first
#secret:second
ZIP=98101
`)

		ogFile, err := secrets(bytes.NewReader(input))
		require.NoError(t, err)
		got, changed := setSecretValue(ogFile, "first", "TOKEN", "bravo")
		require.True(t, changed)
		var sb strings.Builder
		require.NoError(t, got.write(&sb))
		assert.Equal(t, `#secret:first
API_KEY=alpha
# keep this with first
TOKEN=bravo

#secret:second
ZIP=98101
`, sb.String())
	})

	t.Run("adds new secret at end", func(t *testing.T) {
		t.Parallel()

		input := []byte("#secret:first\nAPI_KEY=alpha\n")
		ogFile, err := secrets(bytes.NewReader(input))
		require.NoError(t, err)
		got, changed := setSecretValue(ogFile, "second", "TOKEN", "bravo")
		require.True(t, changed)
		var sb strings.Builder
		require.NoError(t, got.write(&sb))
		assert.Equal(t, `#secret:first
API_KEY=alpha

#secret:second
TOKEN=bravo
`, sb.String())
	})

	t.Run("quotes values with comments", func(t *testing.T) {
		t.Parallel()

		input := []byte("#secret:first\nAPI_KEY=alpha # primary key\n")
		ogFile, err := secrets(bytes.NewReader(input))
		require.NoError(t, err)
		got, changed := setSecretValue(ogFile, "first", "API_KEY", "bravo")
		require.True(t, changed)
		var sb strings.Builder
		require.NoError(t, got.write(&sb))

		assert.Equal(t, "#secret:first\nAPI_KEY=bravo # primary key\n", sb.String())
	})
}
