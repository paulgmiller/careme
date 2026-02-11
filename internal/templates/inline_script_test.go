package templates

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

var (
	scriptTagRE     = regexp.MustCompile(`(?is)<script\b([^>]*)>`)
	scriptSrcAttrRE = regexp.MustCompile(`(?i)\bsrc\s*=`)
	inlineHandlerRE = regexp.MustCompile(`(?is)\son[a-z]+\s*=`)
	javascriptURLRE = regexp.MustCompile(`(?i)javascript:`)
)

func TestTemplatesAvoidInlineJavaScript(t *testing.T) {
	entries, err := fs.ReadDir(htmlFiles, ".")
	if err != nil {
		t.Fatalf("failed to read embedded templates: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}
		content, err := fs.ReadFile(htmlFiles, entry.Name())
		if err != nil {
			t.Fatalf("failed to read template %s: %v", entry.Name(), err)
		}
		src := string(content)

		if inlineHandlerRE.MatchString(src) {
			t.Fatalf("template %s contains inline on* handlers", entry.Name())
		}
		if javascriptURLRE.MatchString(src) {
			t.Fatalf("template %s contains javascript: URL", entry.Name())
		}

		for _, match := range scriptTagRE.FindAllStringSubmatch(src, -1) {
			if len(match) < 2 {
				continue
			}
			attrs := match[1]
			if !scriptSrcAttrRE.MatchString(attrs) {
				t.Fatalf("template %s contains inline <script> without src", entry.Name())
			}
		}
	}
}
