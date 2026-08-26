package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.gen.go")
	source := `package generated

import (
	"encoding/json"
)

type response struct {
	union json.RawMessage
}

func raw(r response) json.RawMessage { return r.union }
`
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))

	require.NoError(t, migrateFile(path))
	migrated, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(migrated), `"encoding/json/jsontext"`)
	assert.Contains(t, string(migrated), `"encoding/json/v2"`)
	assert.Contains(t, string(migrated), "Union jsontext.Value `json:\",embed\"`")
	assert.Contains(t, string(migrated), "return r.Union")
}
