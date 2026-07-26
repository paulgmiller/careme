package kage

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRejectsInvalidSecretName(t *testing.T) {
	t.Parallel()

	_, err := parse(strings.NewReader("#secret:Not_DNS\nKEY=value\n"))
	require.Error(t, err)
}

func TestParse(t *testing.T) {
	t.Parallel()

	got, err := parse(strings.NewReader(`
#secret:first
API_KEY=alpha # primary key
TOKEN="beta # still value" # comment
PATH=with#hash

#secret:second
ZIP=98101
`))
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "first", got[0].Name)
	assert.Equal(t, []Line{
		{Key: "API_KEY", Value: "alpha", Comment: " primary key"},
		{Key: "TOKEN", Value: "beta # still value", Comment: " comment"},
		{Key: "PATH", Value: "with#hash"},
	}, got[0].Lines)
	assert.Equal(t, Secret{Name: "second", Lines: []Line{{Key: "ZIP", Value: "98101"}}}, got[1])
}

func TestParseRejectsInvalidFiles(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"short value":     "#secret:first\nEMPTY=\n",
		"duplicate block": "#secret:first\nAPI_KEY=alpha\n#secret:first\nTOKEN=bravo\n",
		"duplicate key":   "#secret:first\nAPI_KEY=alpha\nAPI_KEY=bravo\n",
		"invalid entry":   "#secret:first\nnot-an-env-line\n",
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parse(strings.NewReader(input))
			require.Error(t, err)
		})
	}
}

func TestFileSetAndWrite(t *testing.T) {
	t.Parallel()

	original, err := parse(strings.NewReader(`#secret:first
API_KEY=alpha # primary key
`))
	require.NoError(t, err)

	_, changed := original.Set("first", "API_KEY", "alpha")
	assert.False(t, changed)

	updated, changed := original.Set("first", "API_KEY", "bravo")
	require.True(t, changed)
	var output bytes.Buffer
	require.NoError(t, updated.write(&output))
	assert.Equal(t, "#secret:first\nAPI_KEY=bravo # primary key\n", output.String())
}
