package kage

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
	"github.com/joho/godotenv"
)

// Load adds values from .env and secrets/envtest to the process environment
// without overwriting variables that are already set.
func Load() error {
	entries, err := readEnv(".env")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	setMissingEnv(entries)

	identities, err := DefaultSSHIdentities()
	if err != nil {
		return fmt.Errorf("load SSH identity: %w", err)
	}
	if len(identities) == 0 {
		return nil
	}

	entries, err = readEncryptedEnv("secrets/envtest", identities)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	setMissingEnv(entries)
	return nil
}

// parseEnv parses a dotenv-style stream. Empty lines and comments are ignored,
// and duplicate keys are rejected.
func parseEnv(r io.Reader) (map[string]string, error) {
	scanner := bufio.NewScanner(r)
	entries := make(map[string]string)
	var lines []string
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		rawLine := scanner.Text()
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			lines = append(lines, rawLine)
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))

		entry, err := parseEnvLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		if _, exists := entries[entry.Key]; exists {
			return nil, fmt.Errorf("line %d: duplicate env key %s", lineNumber, entry.Key)
		}
		entries[entry.Key] = entry.Value
		lines = append(lines, rawLine)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read env: %w", err)
	}
	return godotenv.Unmarshal(strings.Join(lines, "\n"))
}

func readEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open env %q: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	entries, err := parseEnv(file)
	if err != nil {
		return nil, fmt.Errorf("parse env %q: %w", path, err)
	}
	return entries, nil
}

func readEncryptedEnv(path string, identities []age.Identity) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open encrypted env %q: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	reader, err := age.Decrypt(file, identities...)
	if err != nil {
		return nil, fmt.Errorf("decrypt env %q: %w", path, err)
	}
	entries, err := parseEnv(reader)
	if err != nil {
		return nil, fmt.Errorf("parse decrypted env %q: %w", path, err)
	}
	return entries, nil
}

func setMissingEnv(entries map[string]string) {
	for key, value := range entries {
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
}
