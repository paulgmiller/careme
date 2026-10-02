package smithbrothersfarms

import (
	"strings"

	"golang.org/x/net/html"
)

func walk(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		walk(child, visit)
	}
}

func find(n *html.Node, matches func(*html.Node) bool) *html.Node {
	if matches(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := find(child, matches); found != nil {
			return found
		}
	}
	return nil
}

func hasClass(n *html.Node, class string) bool {
	for _, value := range strings.Fields(attribute(n, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func attribute(n *html.Node, key string) string {
	if n != nil {
		for _, attr := range n.Attr {
			if attr.Key == key {
				return attr.Val
			}
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	if n == nil {
		return ""
	}
	var text strings.Builder
	walk(n, func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
	})
	return strings.Join(strings.Fields(text.String()), " ")
}
