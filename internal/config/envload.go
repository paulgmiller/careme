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

	entries, err := decryptDotEnv(path, identities)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	setEnvIfUnset(entries)
	return nil
}

func decryptDotEnv(path string, identities []age.Identity) (map[string]string, error) {
	ciphertext, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("read encrypted env %q: %w", path, err)
	}
	defer ciphertext.Close()

	reader, err := age.Decrypt(ciphertext, identities...)
	if err != nil {
		return nil, fmt.Errorf("decrypt env %q: %w", path, err)
	}

	entries, err := godotenv.Parse(reader)
	if err != nil {
		return nil, fmt.Errorf("parse decrypted env %q: %w", path, err)
	}

	return entries, nil
}

func loadSSHIdentities() ([]age.Identity, error) {
	paths := []string{filepath.Join(".ssh", "id_ed25519")}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		homePath := filepath.Join(home, ".ssh", "id_ed25519")
		if homePath != paths[0] {
			paths = append(paths, homePath)
		}
	}

	for _, path := range paths {
		key, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}

		identity, err := agessh.ParseIdentity(key)
		if err != nil {
			return nil, fmt.Errorf("parse ssh identity %q: %w", path, err)
		}

		return []age.Identity{identity}, nil
	}

	return nil, os.ErrNotExist
}

func setEnvIfUnset(entries map[string]string) {
	// godotenv.Load only accepts filenames; decrypted secrets are in-memory bytes/reader,
	// so we parse and apply them manually while preserving existing process env values.
	for key, value := range entries {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}
