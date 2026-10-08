package main

import (
	"testing"

	"careme/internal/ingredients/gradereview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOptions(t *testing.T) {
	options, err := parseOptions(nil)
	require.NoError(t, err)
	assert.Equal(t, gradereview.EvalOptions{}, options)
	options, err = parseOptions([]string{"-location", " a ", "-cache-version", " v1 "})
	require.NoError(t, err)
	assert.Equal(t, gradereview.EvalOptions{LocationID: "a", CacheVersion: "v1"}, options)
	_, err = parseOptions([]string{"extra"})
	require.Error(t, err)
	_, err = parseOptions([]string{"-unknown"})
	require.Error(t, err)
}
