package recipes

import (
	"bytes"
	"fmt"
	"html/template"

	"github.com/yuin/goldmark"
)

func renderRecipeInstructions(instructions []string) ([]template.HTML, error) {
	rendered := make([]template.HTML, 0, len(instructions))
	for index, instruction := range instructions {
		var output bytes.Buffer
		// Goldmark's default renderer omits raw HTML and dangerous links. Do not
		// enable html.WithUnsafe: these instructions come from the model.
		if err := goldmark.Convert([]byte(instruction), &output); err != nil {
			return nil, fmt.Errorf("render instruction %d: %w", index+1, err)
		}
		rendered = append(rendered, template.HTML(output.String()))
	}
	return rendered, nil
}
