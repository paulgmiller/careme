package produce

import (
	"slices"
	"strings"
	"unicode"

	"careme/internal/ai"

	"golang.org/x/text/unicode/norm"
)

type ReferenceItem struct {
	Term   string `json:"term"`
	Family string `json:"family"`
}

func ReferenceItems() []ReferenceItem {
	terms := DefaultTerms()
	items := make([]ReferenceItem, 0, len(terms))
	for _, term := range terms {
		items = append(items, ReferenceItem{
			Term:   NormalizeTerm(term),
			Family: FamilyForTerm(term),
		})
	}
	return items
}

func ReferenceFamilies() []string {
	seen := make(map[string]struct{})
	var families []string
	for _, item := range ReferenceItems() {
		if item.Family == "" {
			continue
		}
		if _, ok := seen[item.Family]; ok {
			continue
		}
		seen[item.Family] = struct{}{}
		families = append(families, item.Family)
	}
	slices.Sort(families)
	return families
}

func FamilyForTerm(term string) string {
	normalized := NormalizeTerm(term)
	if family, ok := familyOverrides[normalized]; ok {
		return family
	}
	return normalized
}

var familyOverrides = map[string]string{
	"baby arugula":       "arugula",
	"baby bok choy":      "bok choy",
	"baby carrot":        "carrot",
	"baby spinach":       "spinach",
	"baby spring mix":    "spring mix",
	"golden beet":        "beet",
	"green bell pepper":  "bell pepper",
	"green cabbage":      "cabbage",
	"green chili pepper": "chili pepper",
	"green leaf lettuce": "leaf lettuce",
	"italian parsley":    "parsley",
	"mini cucumber":      "cucumber",
	"orange bell pepper": "bell pepper",
	"rainbow carrot":     "carrot",
	"red bell pepper":    "bell pepper",
	"red cabbage":        "cabbage",
	"red chili pepper":   "chili pepper",
	"red leaf lettuce":   "leaf lettuce",
	"seedless cucumber":  "cucumber",
	"yellow bell pepper": "bell pepper",
}

func MatchDescriptions(ingredients []ai.InputIngredient, term string) []string {
	needleTokens := tokens(NormalizeTerm(term))
	if len(needleTokens) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	matches := make([]string, 0)
	for _, ingredient := range ingredients {
		description := strings.TrimSpace(ingredient.Description)
		if description == "" {
			continue
		}
		haystackTokens := tokens(NormalizeTerm(description))
		if !containsAllTokens(haystackTokens, needleTokens) {
			continue
		}
		if _, ok := seen[description]; ok {
			continue
		}
		seen[description] = struct{}{}
		matches = append(matches, description)
	}

	slices.Sort(matches)
	return matches
}

func MatchFamilies(description string) []string {
	haystackTokens := tokens(NormalizeTerm(description))
	if len(haystackTokens) == 0 {
		return nil
	}
	matchedItems := make([]ReferenceItem, 0)
	for _, item := range ReferenceItems() {
		if containsAllTokens(haystackTokens, tokens(item.Term)) {
			matchedItems = append(matchedItems, item)
		}
	}

	seen := make(map[string]struct{})
	var families []string
	for _, item := range mostSpecificMatches(matchedItems) {
		if _, ok := seen[item.Family]; ok {
			continue
		}
		seen[item.Family] = struct{}{}
		families = append(families, item.Family)
	}
	slices.Sort(families)
	return families
}

func mostSpecificMatches(items []ReferenceItem) []ReferenceItem {
	filtered := make([]ReferenceItem, 0, len(items))
	for _, item := range items {
		if hasMoreSpecificMatch(item, items) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func hasMoreSpecificMatch(item ReferenceItem, items []ReferenceItem) bool {
	itemTokens := tokens(item.Term)
	for _, other := range items {
		if other.Term == item.Term {
			continue
		}
		otherTokens := tokens(other.Term)
		if len(otherTokens) <= len(itemTokens) {
			continue
		}
		if containsAllTokens(otherTokens, itemTokens) {
			return true
		}
	}
	return false
}

func NormalizeTerm(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = removeParenthetical(s)
	s = stripDiacritics(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(' ')
	}
	fields := strings.Fields(b.String())
	if len(fields) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(fields))
	for _, field := range fields {
		token := normalizeToken(field)
		if token == "" {
			continue
		}
		normalized = append(normalized, token)
	}
	return strings.Join(normalized, " ")
}

func tokens(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}

func containsAllTokens(haystack []string, needle []string) bool {
	if len(needle) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(haystack))
	for _, token := range haystack {
		set[token] = struct{}{}
	}
	for _, token := range needle {
		if _, ok := set[token]; !ok {
			return false
		}
	}
	return true
}

func removeParenthetical(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func stripDiacritics(s string) string {
	decomposed := norm.NFD.String(s)
	var b strings.Builder
	b.Grow(len(decomposed))
	for _, r := range decomposed {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return norm.NFC.String(b.String())
}

func normalizeToken(s string) string {
	switch {
	case strings.HasSuffix(s, "ies") && len(s) > 3:
		s = s[:len(s)-3] + "y"
	case strings.HasSuffix(s, "oes") && len(s) > 3:
		s = s[:len(s)-2]
	case strings.HasSuffix(s, "ches") || strings.HasSuffix(s, "shes") || strings.HasSuffix(s, "xes") || strings.HasSuffix(s, "zes") || strings.HasSuffix(s, "ses"):
		if len(s) > 4 {
			s = s[:len(s)-2]
		}
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") && len(s) > 2:
		s = s[:len(s)-1]
	}

	switch s {
	case "asparagus":
		return s
	case "brussel":
		return "brussels"
	case "chile":
		return "chili"
	case "kiwifruit":
		return "kiwi"
	case "portobello":
		return "portabella"
	}
	return s
}
