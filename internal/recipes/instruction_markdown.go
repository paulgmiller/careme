package recipes

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/util"
)

var instructionMarkdown = goldmark.New(goldmark.WithParser(parser.NewParser(
	parser.WithBlockParsers(
		util.Prioritized(parser.NewListParser(), 300),
		util.Prioritized(parser.NewListItemParser(), 400),
		util.Prioritized(parser.NewParagraphParser(), 1000),
	),
)))

func renderRecipeInstructions(instructions []string) ([]template.HTML, error) {
	rendered := make([]template.HTML, 0, len(instructions))
	for index, instruction := range instructions {
		var output strings.Builder
		// The parser accepts only paragraphs and lists. Other Markdown remains
		// escaped text, so model output cannot add links, images, or raw HTML.
		if err := instructionMarkdown.Convert([]byte(instruction), &output); err != nil {
			return nil, fmt.Errorf("render instruction %d: %w", index+1, err)
		}
		rendered = append(rendered, template.HTML(output.String()))
	}
	return rendered, nil
}
