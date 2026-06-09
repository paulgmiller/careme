package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

type secretLine struct {
	Key     string
	Value   string
	Comment string
}

type secret struct {
	Name  string
	Lines []secretLine
}

type secretsFile []secret

func (secretVals secretsFile) validate() error {
	secretNames := map[string]bool{}

	for _, secret := range secretVals {
		errs := validation.IsDNS1123Subdomain(secret.Name)
		if len(errs) != 0 {
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
			if len(line.Value) < minSecretValueLength {
				return fmt.Errorf("secret %s/%s must be at least %d characters", secret.Name, line.Key, minSecretValueLength)
			}
		}
	}
	return nil
}

type writerAdapter struct {
	w io.Writer
}

func (a writerAdapter) WriteString(s string) {
	a.w.Write([]byte(s))
}

func (a writerAdapter) WriteByte(c byte) {
	a.w.Write([]byte{c})
}

func (secretVals secretsFile) write(w io.Writer) {
	//ignores all errors?
	out := writerAdapter{w}
	for i, secret := range secretVals {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString("#")
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
			out.WriteString(formatSecretValue(line.Value))
			if line.Comment != "" {
				out.WriteString(" #")
				out.WriteString(line.Comment)
			}
			out.WriteByte('\n')
		}
	}
}

func secrets(r io.Reader) (secretsFile, error) {
	sc := bufio.NewScanner(r)
	var currentSecret *secret
	var secretVals secretsFile

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if len(line) == 0 {
			continue
		}

		if comment, found := strings.CutPrefix(line, "#"); found {
			if secretName, found := strings.CutPrefix(comment, secretCommentPrefix); found {
				if currentSecret != nil {
					secretVals = append(secretVals, *currentSecret)
				}
				currentSecret = &secret{Name: secretName}
				continue
			}
			if currentSecret != nil {
				currentSecret.Lines = append(currentSecret.Lines, secretLine{Comment: comment})
			}
			continue
		}
		if currentSecret == nil {
			return nil, fmt.Errorf("a #%s prefix must come before non commented lines ", secretCommentPrefix)
		}
		entry, err := parseSecretLine(line)
		if err != nil {
			return secretsFile{}, err
		}
		//just a comemnt
		if entry.Key == "" {
			continue
		}
		currentSecret.Lines = append(currentSecret.Lines, entry)
	}
	secretVals = append(secretVals, *currentSecret)

	if err := sc.Err(); err != nil {
		return secretsFile{}, err
	}
	if err := secretVals.validate(); err != nil {
		return secretsFile{}, err
	}
	return secretVals, nil
}

func parseSecretLine(line string) (secretLine, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return secretLine{}, nil
	}

	key, rawValue, found := strings.Cut(trimmed, "=")
	if !found {
		return secretLine{}, fmt.Errorf("invalid secret entry %q", line)
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return secretLine{}, fmt.Errorf("invalid secret entry %q", line)
	}

	trimmedValue := strings.TrimSpace(rawValue)
	if len(trimmedValue) == 0 {
		return secretLine{}, fmt.Errorf("empty value")
	}
	if trimmedValue[0] == '"' {
		close, err := findClosingQuote(trimmedValue)
		if err != nil {
			return secretLine{}, err
		}
		unqouted, err := strconv.Unquote(trimmedValue[0 : close+1])
		if err != nil {
			return secretLine{}, err
		}
		_, comment, _ := strings.Cut(trimmedValue[close:], "#")
		return secretLine{Key: key, Value: unqouted, Comment: comment}, nil
	}
	value, comment, _ := strings.Cut(trimmedValue, " #")
	return secretLine{Key: key, Value: value, Comment: comment}, nil

}

func findClosingQuote(s string) (int, error) {
	escaped := false

	for i := 1; i < len(s); i++ {
		switch {
		case escaped:
			escaped = false

		case s[i] == '\\':
			escaped = true

		case s[i] == '"':
			return i, nil
		}
	}

	return -1, fmt.Errorf("unterminated quoted value")
}
