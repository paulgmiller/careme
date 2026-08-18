package templates

import (
	"html"
	"html/template"
	"strings"
)

// instructionMarkdownHTML renders the instruction subset accepted from the
// model: paragraphs and "- " bullet lists. All model text is HTML-escaped.
func instructionMarkdownHTML(markdown string) template.HTML {
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	markdown = strings.ReplaceAll(markdown, "\r", "\n")

	var output strings.Builder
	var paragraph []string
	inList := false

	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		output.WriteString("<p>")
		output.WriteString(html.EscapeString(strings.Join(paragraph, " ")))
		output.WriteString("</p>")
		paragraph = paragraph[:0]
	}
	closeList := func() {
		if !inList {
			return
		}
		output.WriteString("</ul>")
		inList = false
	}

	for line := range strings.SplitSeq(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if item, ok := strings.CutPrefix(trimmed, "- "); ok && strings.TrimSpace(item) != "" {
			flushParagraph()
			if !inList {
				output.WriteString("<ul>")
				inList = true
			}
			output.WriteString("<li>")
			output.WriteString(html.EscapeString(strings.TrimSpace(item)))
			output.WriteString("</li>")
			continue
		}

		closeList()
		if trimmed == "" {
			flushParagraph()
			continue
		}
		paragraph = append(paragraph, trimmed)
	}

	closeList()
	flushParagraph()
	return template.HTML(output.String())
}
