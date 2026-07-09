package googleads

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMicroDegreesRoundsToMillionths(t *testing.T) {
	assert.Equal(t, int64(47628826), MicroDegrees(47.6288264))
	assert.Equal(t, int64(-122144646), MicroDegrees(-122.1446462))
}
