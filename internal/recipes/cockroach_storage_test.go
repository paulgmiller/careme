package recipes

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCockroachStorageRequiresDatabaseURL(t *testing.T) {
	s, err := NewCockroachStorage(t.Context(), "")
	require.ErrorContains(t, err, "open recipe storage")
	require.Nil(t, s)
}
