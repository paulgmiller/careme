package recipes

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/util"
)

var recipeMarkdown = goldmark.New(goldmark.WithParser(parser.NewParser(
	parser.WithBlockParsers(
		util.Prioritized(parser.NewListParser(), 300),
		util.Prioritized(parser.NewListItemParser(), 400),
		util.Prioritized(parser.NewParagraphParser(), 1000),
	),
	parser.WithInlineParsers(util.Prioritized(parser.NewEmphasisParser(), 500)),
)))

func renderRecipeInstructions(instructions []string) ([]template.HTML, error) {
	rendered := make([]template.HTML, 0, len(instructions))
	for index, instruction := range instructions {
		html, err := renderRecipeMarkdown(instruction)
		if err != nil {
			return nil, fmt.Errorf("render instruction %d: %w", index+1, err)
		}
		rendered = append(rendered, html)
	}
	return rendered, nil
}

func renderRecipeMarkdown(text string) (template.HTML, error) {
	var output strings.Builder
	// Only paragraphs, lists, and emphasis are parsed. Links, images, and
	// raw HTML remain escaped text.
	if err := recipeMarkdown.Convert([]byte(text), &output); err != nil {
		return "", err
	}
	return template.HTML(output.String()), nil
}
