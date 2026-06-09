package main

import (
	"errors"
	"fmt"
	"io"
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
