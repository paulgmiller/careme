// Command jsonv2gen updates oapi-codegen output to use encoding/json/v2.
package main

import (
	"fmt"
	"go/format"
	"os"
	"regexp"
	"strings"
)

var rawMessageField = regexp.MustCompile(`\bunion jsontext\.Value`)

const legacyJSONImport = `"encoding/json"`

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: jsonv2gen <generated-go-file>")
		os.Exit(2)
	}
	if err := migrateFile(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func migrateFile(path string) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	migrated := strings.Replace(string(source), legacyJSONImport, `"encoding/json/jsontext"
	"encoding/json/v2"`, 1)
	migrated = strings.ReplaceAll(migrated, "json.RawMessage", "jsontext.Value")
	migrated = rawMessageField.ReplaceAllString(migrated, "Union jsontext.Value `json:\",embed\"`")
	migrated = strings.ReplaceAll(migrated, ".union", ".Union")

	formatted, err := format.Source([]byte(migrated))
	if err != nil {
		return fmt.Errorf("format migrated %s: %w", path, err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
