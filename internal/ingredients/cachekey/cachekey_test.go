package cachekey

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStaplesSignatureWithoutRegistry(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "test", StaplesSignature("loc-123"))
	assert.Equal(t, "test", StaplesSignature("another-store"))
}
