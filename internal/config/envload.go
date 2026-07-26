package config

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"careme/pkg/kage"
)

var envLoadOnce sync.Once

// asumes you are running from root of repo.
func loadRuntimeEnv() error {
	var loadErr error
	envLoadOnce.Do(func() {
		entries, err := kage.ReadEnv(".env")
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				loadErr = fmt.Errorf("load .env: %w", err)
				return
			}
		} else {
			setMissingEnv(entries)
		}

		// TODO make this settable with env var :)
		if err := LoadEncryptedEnv("secrets/envtest"); err != nil {
			loadErr = err
		}
	})
	return loadErr
}

func LoadEncryptedEnv(path string) error {
	identities, err := kage.DefaultSSHIdentities()
	if err != nil {
		return fmt.Errorf("load ssh identity for %q: %w", path, err)
	}
	if len(identities) == 0 {
		// should we log
		return nil
	}

	entries, err := kage.ReadEncryptedEnv(path, identities)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	setMissingEnv(entries)
	return nil
}

func setMissingEnv(entries map[string]string) {
	for key, value := range entries {
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}
