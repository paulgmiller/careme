package kage_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"careme/pkg/kage"

	"filippo.io/age"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnv(t *testing.T) {
	t.Parallel()

	entries, err := kage.ParseEnv(strings.NewReader(`
# ignored
export PLAIN=value
QUOTED="value with spaces" # explanation
SINGLE='literal # value'
HASH=value#part
EMPTY=
`))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"PLAIN":  "value",
		"QUOTED": "value with spaces",
		"SINGLE": "literal # value",
		"HASH":   "value#part",
		"EMPTY":  "",
	}, entries)
}

func TestReadEnvAndReadEncryptedEnvUseSameParser(t *testing.T) {
	t.Parallel()

	const plaintext = "PLAIN=value\nQUOTED=\"value with spaces\" # explanation\nEMPTY=\n"
	directory := t.TempDir()
	plainPath := filepath.Join(directory, ".env")
	require.NoError(t, os.WriteFile(plainPath, []byte(plaintext), 0o600))

	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	encryptedPath := filepath.Join(directory, "env.age")
	require.NoError(t, encrypt(t, encryptedPath, plaintext, identity.Recipient()))

	plainEntries, err := kage.ReadEnv(plainPath)
	require.NoError(t, err)
	encryptedEntries, err := kage.ReadEncryptedEnv(encryptedPath, []age.Identity{identity})
	require.NoError(t, err)
	assert.Equal(t, plainEntries, encryptedEntries)
}

func TestParseEnvRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()

	_, err := kage.ParseEnv(strings.NewReader("KEY=first\nKEY=second\n"))
	require.ErrorContains(t, err, "duplicate env key KEY")
}

func encrypt(t *testing.T, path, plaintext string, recipient age.Recipient) error {
	t.Helper()

	var ciphertext bytes.Buffer
	writer, err := age.Encrypt(&ciphertext, recipient)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(writer, plaintext); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, ciphertext.Bytes(), 0o600)
}
