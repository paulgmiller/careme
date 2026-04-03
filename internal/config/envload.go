package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"filippo.io/age"
	agessh "filippo.io/age/agessh"
	"github.com/joho/godotenv"
)

var envLoadOnce sync.Once

func loadRuntimeEnv() error {
	var loadErr error
	envLoadOnce.Do(func() {
		if err := loadDotEnv(".env"); err != nil {
			loadErr = err
			return
		}

		if err := loadEncryptedEnv("secrets/envtest"); err != nil {
			loadErr = err
		}
	})
	return loadErr
}

func loadDotEnv(path string) error {
	resolvedPath, err := findPathUpToGitRoot(path)
	if err != nil {
		return err
	}
	if resolvedPath == "" {
		return nil
	}

	if err := godotenv.Load(resolvedPath); err != nil {
		return fmt.Errorf("load %q: %w", resolvedPath, err)
	}

	return nil
}

func loadEncryptedEnv(path string) error {
	resolvedPath, err := findPathUpToGitRoot(path)
	if err != nil {
		return err
	}
	if resolvedPath == "" {
		return nil
	}

	identities, err := loadSSHIdentities()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("load ssh identity for %q: %w", path, err)
	}

	entries, err := decryptDotEnv(resolvedPath, identities)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	mergeEnv(entries)
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

func findPathUpToGitRoot(path string) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for {
		candidate := filepath.Join(dir, path)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat %q: %w", candidate, err)
		}

		gitMarker := filepath.Join(dir, ".git")
		if _, err := os.Stat(gitMarker); err == nil {
			return "", nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("stat %q: %w", gitMarker, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
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

func mergeEnv(entries map[string]string) {
	for key, value := range entries {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}
