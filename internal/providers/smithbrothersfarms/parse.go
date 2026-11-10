package smithbrothersfarms

import (
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"net/url"
	"path"
	"strconv"
	"strings"

	"careme/internal/ai"

	"golang.org/x/net/html"
)

// Parse extracts catalog cards, omitting the whole boxes expanded by FetchStaples.
func Parse(r io.Reader) ([]ai.InputIngredient, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("read category HTML: %w", err)
	}
	catalog := find(doc, func(n *html.Node) bool { return hasClass(n, "catalog-scroll") })
	if catalog == nil || find(catalog, func(n *html.Node) bool { return hasClass(n, "item-grid") }) == nil {
		return nil, fmt.Errorf("category HTML has no product grid")
	}
	category := nodeText(find(catalog, func(n *html.Node) bool { return n.Data == "h1" }))
	if category == "" {
		return nil, fmt.Errorf("category HTML has no heading")
	}
	var items []ai.InputIngredient
	var parseErr error
	subcategory := ""
	cards := 0
	walk(catalog, func(n *html.Node) {
		if parseErr != nil {
			return
		}
		if n.Data == "h2" {
			subcategory = nodeText(n)
		}
		if !hasClass(n, "product-item") {
			return
		}
		cards++
		title := find(n, func(n *html.Node) bool { return hasClass(n, "product-title") })
		link := find(n, func(n *html.Node) bool { return n.Data == "a" && attribute(n, "href") != "" })
		if isHarvestBox(attribute(link, "href")) {
			return
		}
		item, err := parseCard(n, title, category, subcategory)
		if err != nil {
			parseErr = fmt.Errorf("product card %d: %w", cards, err)
			return
		}
		items = append(items, item)
	})
	if parseErr != nil {
		return nil, parseErr
	}
	if cards == 0 {
		return nil, fmt.Errorf("category HTML has no product cards")
	}
	return items, nil
}

func isHarvestBox(href string) bool {
	u, err := url.Parse(href)
	if err != nil {
		return false
	}
	for _, box := range harvestBoxes {
		if strings.Trim(u.Path, "/") == box.slug {
			return true
		}
	}
	return false
}

func parseCard(card, title *html.Node, category, subcategory string) (ai.InputIngredient, error) {
	item := ai.NormalizeInputIngredient(ai.InputIngredient{
		ProductID: attribute(card, "data-productid"), Description: nodeText(title),
		Brand:       nodeText(find(card, func(n *html.Node) bool { return hasClass(n, "brand-text") })),
		Size:        strings.TrimSpace(nodeText(find(card, func(n *html.Node) bool { return hasClass(n, "web-units") })) + " " + nodeText(find(card, func(n *html.Node) bool { return hasClass(n, "web-unit-of-measure") }))),
		AisleNumber: category, Categories: []string{category},
	})
	if item.ProductID == "" || item.Description == "" {
		return ai.InputIngredient{}, fmt.Errorf("missing product ID or title")
	}
	if subcategory != "" {
		item.Categories = append(item.Categories, subcategory)
	}
	prices := find(card, func(n *html.Node) bool { return hasClass(n, "prices") })
	priceNode := find(card, func(n *html.Node) bool { return hasClass(n, "price") || hasClass(n, "actual-price") })
	if prices == nil {
		return ai.InputIngredient{}, fmt.Errorf("product %s has no prices", item.ProductID)
	}
	price, err := parsePrice(nodeText(priceNode))
	if err != nil {
		return ai.InputIngredient{}, fmt.Errorf("product %s price: %w", item.ProductID, err)
	}
	item.PriceRegular = &price
	old := find(prices, func(n *html.Node) bool { return hasClass(n, "old-price") })
	if old != nil {
		regular, err := parsePrice(nodeText(old))
		if err != nil {
			return ai.InputIngredient{}, fmt.Errorf("product %s regular price: %w", item.ProductID, err)
		}
		if regular > price {
			item.PriceRegular, item.PriceSale = &regular, &price
		}
	}
	return item, nil
}

func parsePrice(text string) (float32, error) {
	value := strings.ReplaceAll(strings.TrimPrefix(text, "$"), ",", "")
	price, err := strconv.ParseFloat(value, 32)
	if err != nil || math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
		return 0, fmt.Errorf("invalid price %q", text)
	}
	return float32(price), nil
}

// ParseHarvestBox reads only the seasonal list inside the product description.
func ParseHarvestBox(r io.Reader, boxURL, name string) ([]ai.InputIngredient, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, fmt.Errorf("read harvest box HTML: %w", err)
	}
	description := find(doc, func(n *html.Node) bool { return hasClass(n, "full-description") })
	if description == nil {
		return nil, fmt.Errorf("harvest box HTML has no full description")
	}
	list := find(description, func(n *html.Node) bool { return n.Data == "ul" })
	if list == nil {
		return nil, fmt.Errorf("harvest box description has no ingredient list")
	}
	var items []ai.InputIngredient
	for n := list.FirstChild; n != nil; n = n.NextSibling {
		if n.Data != "li" {
			continue
		}
		text := nodeText(n)
		if text == "" {
			return nil, fmt.Errorf("harvest box contains an empty ingredient")
		}
		items = append(items, ai.InputIngredient{
			ProductID:   fmt.Sprintf("%s%s_%x", LocationIDPrefix, path.Base(boxURL), sha256.Sum256([]byte(text))),
			Description: name + " — " + text, Brand: "Smith Brothers Farms", AisleNumber: "Produce",
			Categories: []string{"Produce", name},
			Grade:      &ai.IngredientGrade{Score: 10, Reason: "Fresh produce from the Smith Brothers Farms harvest box list."},
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("harvest box ingredient list is empty")
	}
	return items, nil
}
