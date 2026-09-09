package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Revision returns the source control revision embedded in the running binary.
// It returns an empty string when the binary does not contain VCS metadata,
// such as some local builds produced by go run.
func Revision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return revision(info)
}

func revision(info *debug.BuildInfo) string {
	for _, setting := range info.Settings {
		if setting.Key != "vcs.revision" {
			continue
		}
		revision := strings.TrimSpace(setting.Value)
		if revision != "" {
			return revision
		}
	}

	return ""
}
