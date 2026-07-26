package kage_test

import (
	"strings"
	"testing"

	"careme/pkg/kage"

	"github.com/stretchr/testify/require"
)

func TestParseRejectsInvalidSecretName(t *testing.T) {
	t.Parallel()

	_, err := kage.Parse(strings.NewReader("#secret:Not_DNS\nKEY=value\n"))
	require.Error(t, err)
}
