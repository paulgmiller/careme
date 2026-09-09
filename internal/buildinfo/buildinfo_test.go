package buildinfo

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRevision(t *testing.T) {
	t.Parallel()

	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.time", Value: "2026-09-08T12:00:00Z"},
		{Key: "vcs.revision", Value: " 0123456789abcdef "},
	}}

	assert.Equal(t, "0123456789abcdef", revision(info))
}

func TestRevisionReturnsUnknownWhenMissing(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "unknown", revision(&debug.BuildInfo{}))
}
