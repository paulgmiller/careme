package config

import (
	"sync"

	"careme/pkg/kage"
)

var envLoadOnce sync.Once

// asumes you are running from root of repo.
func loadRuntimeEnv() error {
	var loadErr error
	envLoadOnce.Do(func() {
		loadErr = kage.Load()
	})
	return loadErr
}
