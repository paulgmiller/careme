package mnfoodclub

import (
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"careme/internal/ai"

	"golang.org/x/net/html"
)

// Parse extracts product cards from a category page without fetching other pages.
func Parse(r io.Reader) ([]ai.InputIngredient, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("read category HTML: %w", err)
	}
	grid := find(doc, func(n *html.Node) bool { return hasClass(n, "productGrid") })
	if grid == nil {
		return nil, fmt.Errorf("category HTML has no product grid")
	}
	heading := find(doc, func(n *html.Node) bool { return n.Data == "h1" && hasClass(n, "page-heading") })
	category := nodeText(heading)
	ingredients := make([]ai.InputIngredient, 0)
	var parseErr error
	walk(grid, func(n *html.Node) {
		if parseErr != nil || n.Data != "article" || !hasClass(n, "card") {
			return
		}
		ingredient, err := parseCard(n, category)
		if err != nil {
			parseErr = fmt.Errorf("product card %d: %w", len(ingredients)+1, err)
			return
		}
		ingredients = append(ingredients, ingredient)
	})
	if parseErr != nil {
		return nil, parseErr
	}
	return ingredients, nil
}

func parseCard(card *html.Node, category string) (ai.InputIngredient, error) {
	idNode := find(card, func(n *html.Node) bool { return attribute(n, "data-product-id") != "" })
	title := find(card, func(n *html.Node) bool { return hasClass(n, "card-title") })
	brand := find(card, func(n *html.Node) bool { return attribute(n, "data-test-info-type") == "brandName" })
	ingredient := ai.NormalizeInputIngredient(ai.InputIngredient{
		ProductID: attribute(idNode, "data-product-id"), Description: nodeText(title),
		Brand: nodeText(brand), AisleNumber: category,
	})
	if ingredient.ProductID == "" || ingredient.Description == "" {
		return ai.InputIngredient{}, fmt.Errorf("missing product ID or title")
	}
	if category != "" {
		ingredient.Categories = []string{category}
	}
	priceNode := find(card, func(n *html.Node) bool { return hasAttribute(n, "data-product-price-without-tax") })
	price, err := parsePrice(nodeText(priceNode))
	if err != nil {
		return ai.InputIngredient{}, fmt.Errorf("product %s price: %w", ingredient.ProductID, err)
	}
	ingredient.PriceRegular = &price
	regularNode := find(card, func(n *html.Node) bool { return hasAttribute(n, "data-product-non-sale-price-without-tax") })
	if text := nodeText(regularNode); text != "" {
		regular, err := parsePrice(text)
		if err != nil {
			return ai.InputIngredient{}, fmt.Errorf("product %s non-sale price: %w", ingredient.ProductID, err)
		}
		if regular > price {
			ingredient.PriceRegular = &regular
			ingredient.PriceSale = &price
		}
	}
	return ingredient, nil
}

func parsePrice(text string) (float32, error) {
	// Variant listings expose a range; InputIngredient stores the starting price.
	if low, high, ok := strings.Cut(text, " - "); ok {
		minimum, err := parseSinglePrice(low)
		if err != nil {
			return 0, err
		}
		maximum, err := parseSinglePrice(high)
		if err != nil || maximum < minimum {
			return 0, fmt.Errorf("invalid price range %q", text)
		}
		return minimum, nil
	}
	return parseSinglePrice(text)
}

func parseSinglePrice(text string) (float32, error) {
	value := strings.ReplaceAll(strings.TrimPrefix(text, "$"), ",", "")
	price, err := strconv.ParseFloat(value, 32)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
		return 0, fmt.Errorf("invalid price %q", text)
	}
	return float32(price), nil
}

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

func hasAttribute(n *html.Node, key string) bool {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
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
