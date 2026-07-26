package kage

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	secretCommentPrefix  = "secret:"
	MinSecretValueLength = 5
)

// Line is one key/value or comment line in a kage secret block.
type Line struct {
	Key     string
	Value   string
	Comment string
}

// Secret is a named group of environment entries.
type Secret struct {
	Name  string
	Lines []Line
}

// File is the plaintext representation of a kage secrets file.
type File []Secret

// Parse reads the plaintext representation of a kage secrets file.
func Parse(r io.Reader) (File, error) {
	scanner := bufio.NewScanner(r)
	var currentSecret *Secret
	var secrets File

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if comment, found := strings.CutPrefix(line, "#"); found {
			if secretName, isSecret := strings.CutPrefix(comment, secretCommentPrefix); isSecret {
				if currentSecret != nil {
					secrets = append(secrets, *currentSecret)
				}
				currentSecret = &Secret{Name: secretName}
				continue
			}
			if currentSecret != nil {
				currentSecret.Lines = append(currentSecret.Lines, Line{Comment: comment})
			}
			continue
		}
		if currentSecret == nil {
			return nil, fmt.Errorf("a #%s prefix must come before non-comment lines", secretCommentPrefix)
		}
		entry, err := parseEnvLine(line)
		if err != nil {
			return nil, err
		}
		currentSecret.Lines = append(currentSecret.Lines, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read secrets: %w", err)
	}
	if currentSecret == nil {
		return nil, fmt.Errorf("no #%s block found", secretCommentPrefix)
	}
	secrets = append(secrets, *currentSecret)

	if err := secrets.Validate(); err != nil {
		return nil, err
	}
	return secrets, nil
}

// Validate checks all invariants required by the kage file format.
func (secrets File) Validate() error {
	secretNames := map[string]bool{}

	for _, secret := range secrets {
		if errs := validation.IsDNS1123Subdomain(secret.Name); len(errs) != 0 {
			return errors.New(strings.Join(errs, ";"))
		}
		if secretNames[secret.Name] {
			return fmt.Errorf("duplicate secret name %s", secret.Name)
		}
		secretNames[secret.Name] = true

		keys := map[string]bool{}
		for _, line := range secret.Lines {
			if line.Key == "" {
				continue
			}
			if keys[line.Key] {
				return fmt.Errorf("duplicate secret key %s", line.Key)
			}
			keys[line.Key] = true
			if len(line.Value) < MinSecretValueLength {
				return fmt.Errorf(
					"secret %s/%s must be at least %d characters",
					secret.Name,
					line.Key,
					MinSecretValueLength,
				)
			}
		}
	}
	return nil
}

// Write serializes a plaintext kage secrets file.
func (secrets File) Write(w io.Writer) error {
	var out bytes.Buffer
	for i, secret := range secrets {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteByte('#')
		out.WriteString(secretCommentPrefix)
		out.WriteString(secret.Name)
		out.WriteByte('\n')
		for _, line := range secret.Lines {
			if line.Key == "" {
				if line.Comment != "" {
					out.WriteByte('#')
					out.WriteString(line.Comment)
					out.WriteByte('\n')
				}
				continue
			}
			out.WriteString(line.Key)
			out.WriteByte('=')
			out.WriteString(formatEnvValue(line.Value))
			if line.Comment != "" {
				out.WriteString(" #")
				out.WriteString(line.Comment)
			}
			out.WriteByte('\n')
		}
	}
	_, err := w.Write(out.Bytes())
	return err
}

// Set adds or updates an entry and reports whether the file changed.
func (secrets File) Set(secretName, key, value string) (File, bool) {
	var output File
	var secretFound bool
	for _, existingSecret := range secrets {
		if existingSecret.Name != secretName {
			output = append(output, existingSecret)
			continue
		}
		secretFound = true
		var lineFound bool
		for i, line := range existingSecret.Lines {
			if line.Key != key {
				continue
			}
			if line.Value == value {
				return nil, false
			}
			line.Value = value
			existingSecret.Lines[i] = line
			lineFound = true
		}
		if !lineFound {
			existingSecret.Lines = append(existingSecret.Lines, Line{Key: key, Value: value})
		}
		output = append(output, existingSecret)
	}
	if !secretFound {
		output = append(output, Secret{Name: secretName, Lines: []Line{{Key: key, Value: value}}})
	}
	return output, true
}

func parseEnvLine(line string) (Line, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return Line{}, fmt.Errorf("empty env entry")
	}

	key, rawValue, found := strings.Cut(trimmed, "=")
	if !found {
		return Line{}, fmt.Errorf("invalid env entry %q", line)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return Line{}, fmt.Errorf("invalid env entry %q", line)
	}

	trimmedValue := strings.TrimSpace(rawValue)
	if trimmedValue == "" {
		return Line{Key: key}, nil
	}
	if trimmedValue[0] == '"' || trimmedValue[0] == '\'' {
		quote := trimmedValue[0]
		close, err := findClosingQuote(trimmedValue, quote)
		if err != nil {
			return Line{}, err
		}
		trailing := strings.TrimSpace(trimmedValue[close+1:])
		if trailing != "" && !strings.HasPrefix(trailing, "#") {
			return Line{}, fmt.Errorf("unexpected content after quoted value")
		}

		unquoted := trimmedValue[1:close]
		if quote == '"' {
			unquoted, err = strconv.Unquote(trimmedValue[:close+1])
			if err != nil {
				return Line{}, err
			}
		}
		_, comment, _ := strings.Cut(trailing, "#")
		return Line{Key: key, Value: unquoted, Comment: comment}, nil
	}
	value, comment, _ := strings.Cut(trimmedValue, " #")
	return Line{Key: key, Value: value, Comment: comment}, nil
}

func formatEnvValue(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\n\r#\"'") {
		return strconv.Quote(value)
	}
	return value
}

func findClosingQuote(value string, quote byte) (int, error) {
	escaped := false
	for i := 1; i < len(value); i++ {
		switch {
		case escaped:
			escaped = false
		case quote == '"' && value[i] == '\\':
			escaped = true
		case value[i] == quote:
			return i, nil
		}
	}
	return -1, fmt.Errorf("unterminated quoted value")
}
