package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"filippo.io/age"
	"filippo.io/age/agessh"
	"github.com/joho/godotenv"
)

var envLoadOnce sync.Once

// asumes you are running from root of repo.
func loadRuntimeEnv() error {
	var loadErr error
	envLoadOnce.Do(func() {
		// does not error on not found (or any file.open error)
		if err := godotenv.Load(); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				loadErr = fmt.Errorf("load .env: %w", err)
				return
			}
		}

		if err := loadEncryptedEnv("secrets/envtest"); err != nil {
			loadErr = err
		}
	})
	return loadErr
}

func loadEncryptedEnv(path string) error {
	identities, err := loadSSHIdentities()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("load ssh identity for %q: %w", path, err)
	}

	return decryptDotEnv(path, identities)
}

func decryptDotEnv(path string, identities []age.Identity) error {
	ciphertext, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read encrypted env %q: %w", path, err)
	}
	defer func() {
		_ = ciphertext.Close()
	}()

	reader, err := age.Decrypt(ciphertext, identities...)
	if err != nil {
		return fmt.Errorf("decrypt env %q: %w", path, err)
	}

	// no load for read so manually merge in entries from parse
	entries, err := godotenv.Parse(reader)
	if err != nil {
		return fmt.Errorf("parse decrypted env %q: %w", path, err)
	}
	for key, value := range entries {
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
	return nil
}

func loadSSHIdentities() ([]age.Identity, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return []age.Identity{}, nil
	}
	path := filepath.Join(home, ".ssh", "id_ed25519")

	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	identity, err := agessh.ParseIdentity(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh identity %q: %w", path, err)
	}

	return []age.Identity{identity}, nil
}
