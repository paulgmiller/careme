package kage

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

// ParseEnv parses a dotenv-style stream. Empty lines and comments are ignored,
// and duplicate keys are rejected.
func ParseEnv(r io.Reader) (map[string]string, error) {
	scanner := bufio.NewScanner(r)
	entries := make(map[string]string)
	lineNumber := 0

	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
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
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read env: %w", err)
	}
	return entries, nil
}

// ReadEnv reads and parses a regular dotenv-style file.
func ReadEnv(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open env %q: %w", path, err)
	}
	defer func() {
		_ = file.Close()
	}()

	entries, err := ParseEnv(file)
	if err != nil {
		return nil, fmt.Errorf("parse env %q: %w", path, err)
	}
	return entries, nil
}

// ReadEncryptedEnv decrypts an age-encrypted file and parses its plaintext
// with the same parser used by ReadEnv.
func ReadEncryptedEnv(path string, identities []age.Identity) (map[string]string, error) {
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
	entries, err := ParseEnv(reader)
	if err != nil {
		return nil, fmt.Errorf("parse decrypted env %q: %w", path, err)
	}
	return entries, nil
}
