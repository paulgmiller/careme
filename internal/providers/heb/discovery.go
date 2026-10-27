package heb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	htmlstd "html"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type StorePage struct {
	URL        string
	Country    string
	State      string
	City       string
	Slug       string
	URLStoreID string
}

type StoreSummary struct {
	ID      string   `json:"id"`
	StoreID string   `json:"store_id"`
	Name    string   `json:"name"`
	Address string   `json:"address"`
	City    string   `json:"city"`
	State   string   `json:"state"`
	ZipCode string   `json:"zip_code"`
	URL     string   `json:"url"`
	Lat     *float64 `json:"lat,omitempty"`
	Lon     *float64 `json:"lon,omitempty"`
}

type storeFields struct {
	Name    string
	Address string
	City    string
	State   string
	ZipCode string
	StoreID string
	Lat     *float64
	Lon     *float64
	score   int
}

type htmlSignals struct {
	BodyText    string
	Title       string
	H1          string
	AddressText string
	Scripts     []scriptBlock
	Meta        map[string]string
}

type scriptBlock struct {
	Type    string
	Content string
}

var (
	storePathIDRe     = regexp.MustCompile(`-(\d+)$`)
	corporateIDRe     = regexp.MustCompile(`(?i)(?:corporate|store)\s*#\s*(\d+)`)
	geoPositionMetaRe = regexp.MustCompile(`(?i)^\s*([+-]?\d+(?:\.\d+)?)\s*;\s*([+-]?\d+(?:\.\d+)?)\s*$`)
	jsonLatitudeRe    = regexp.MustCompile(`(?i)"(?:latitude|lat)"\s*:\s*"?([+-]?\d+(?:\.\d+)?)"?`)
	jsonLongitudeRe   = regexp.MustCompile(`(?i)"(?:longitude|lng|lon)"\s*:\s*"?([+-]?\d+(?:\.\d+)?)"?`)
	jsonStreetRe      = regexp.MustCompile(`(?i)"(?:streetAddress|address1|addressLine1|line1|street)"\s*:\s*"([^"]+)"`)
	jsonCityRe        = regexp.MustCompile(`(?i)"(?:addressLocality|city)"\s*:\s*"([^"]+)"`)
	jsonStateRe       = regexp.MustCompile(`(?i)"(?:addressRegion|state|region)"\s*:\s*"([a-z]{2})"`)
	jsonZipRe         = regexp.MustCompile(`(?i)"(?:postalCode|zipCode|zip)"\s*:\s*"(\d{5}(?:-\d{4})?)"`)
	jsonStoreIDRe     = regexp.MustCompile(`(?i)"(?:branchCode|storeCode|storeID|storeId|locationId|identifier)"\s*:\s*"?(\d+)"?`)
	directionsCoordRe = regexp.MustCompile(`(?i)(?:destination|ll|q|center)=([+-]?\d+(?:\.\d+)?),([+-]?\d+(?:\.\d+)?)`)
	addressLineRe     = regexp.MustCompile(`(?i)(\d[0-9a-z .#-]+?)\s+([a-z][a-z .'-]+),\s*([a-z]{2})\s+(\d{5}(?:-\d{4})?)`)
)

func IsID(locationID string) bool {
	locationID = strings.TrimSpace(locationID)
	if !strings.HasPrefix(locationID, LocationIDPrefix) {
		return false
	}
	return isDigits(strings.TrimPrefix(locationID, LocationIDPrefix))
}

func FilterStorePages(urls []string) []StorePage {
	pages := make([]StorePage, 0, len(urls))
	seen := make(map[string]struct{}, len(urls))
	for _, rawURL := range urls {
		page, ok := ParseStorePageURL(rawURL)
		if !ok {
			continue
		}
		if _, exists := seen[page.URL]; exists {
			continue
		}
		seen[page.URL] = struct{}{}
		pages = append(pages, page)
	}
	return pages
}

// ParseStorePageURL accepts only HEB store landing pages.
// Expected path shape is:
//
//	/heb-store/US/<state>/<city>/<store-slug>-<store-number>
//
// For example:
//
//	/heb-store/US/tx/robstown/robstown-h-e-b-22
//
// City pages, category pages, and store paths without the trailing numeric
// store identifier are rejected.
func ParseStorePageURL(rawURL string) (StorePage, bool) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return StorePage{}, false
	}

	host := strings.TrimSpace(u.Host)
	if host == "" {
		return StorePage{}, false
	}

	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segments) != 5 {
		return StorePage{}, false
	}
	if !strings.EqualFold(segments[0], "heb-store") {
		return StorePage{}, false
	}

	slug := strings.TrimSpace(segments[4])
	matches := storePathIDRe.FindStringSubmatch(slug)
	if len(matches) != 2 {
		return StorePage{}, false
	}

	country := strings.ToUpper(strings.TrimSpace(segments[1]))
	state := strings.ToUpper(strings.TrimSpace(segments[2]))
	city := strings.TrimSpace(segments[3])
	if country == "" || state == "" || city == "" {
		return StorePage{}, false
	}

	return StorePage{
		URL:        rawURL,
		Country:    country,
		State:      state,
		City:       city,
		Slug:       slug,
		URLStoreID: matches[1],
	}, true
}

func FetchStoreSummary(ctx context.Context, client *http.Client, pageURL string) (*StoreSummary, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build store page request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get store page: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get store page: status %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read store page: %w", err)
	}

	return ExtractStoreSummary(pageURL, body)
}

func ExtractStoreSummary(pageURL string, body []byte) (*StoreSummary, error) {
	page, ok := ParseStorePageURL(pageURL)
	if !ok {
		return nil, fmt.Errorf("store page URL %q is invalid", pageURL)
	}

	signals := collectHTMLSignals(body)
	fields := extractFieldsFromJSONLD(signals.Scripts)
	fields = fillMissingFields(fields, extractFieldsFromStructuredText(body, signals))

	fields.StoreID = resolveStoreID(page.URLStoreID, extractStoreID(signals.BodyText), fields.StoreID)
	if fields.Name == "" {
		fields.Name = strings.TrimSpace(signals.H1)
	}
	if fields.Name == "" {
		fields.Name = titleName(signals.Title)
	}
	if fields.Name == "" {
		fields.Name = displayNameFromSlug(page.Slug)
	}
	if fields.Address == "" || fields.City == "" || fields.State == "" || fields.ZipCode == "" {
		addr, city, state, zip := extractAddress(signals, page.City)
		if fields.Address == "" {
			fields.Address = addr
		}
		if fields.City == "" {
			fields.City = city
		}
		if fields.State == "" {
			fields.State = state
		}
		if fields.ZipCode == "" {
			fields.ZipCode = zip
		}
	}
	if fields.State == "" {
		fields.State = page.State
	}
	if fields.City == "" {
		fields.City = titleCaseSlug(page.City)
	}
	if fields.Lat == nil || fields.Lon == nil {
		lat, lon := extractCoordinates(signals, body)
		if fields.Lat == nil {
			fields.Lat = lat
		}
		if fields.Lon == nil {
			fields.Lon = lon
		}
	}

	fields.Name = normalizeWhitespace(fields.Name)
	fields.Address = normalizeWhitespace(fields.Address)
	fields.City = normalizeWhitespace(fields.City)
	fields.State = strings.ToUpper(strings.TrimSpace(fields.State))
	fields.ZipCode = strings.TrimSpace(fields.ZipCode)
	fields.StoreID = strings.TrimSpace(fields.StoreID)

	if fields.StoreID == "" {
		return nil, fmt.Errorf("store id not found")
	}
	if fields.Name == "" {
		return nil, fmt.Errorf("store name not found")
	}
	if fields.Address == "" || fields.State == "" || fields.ZipCode == "" {
		return nil, fmt.Errorf("store address not found")
	}

	return &StoreSummary{
		ID:      LocationIDPrefix + fields.StoreID,
		StoreID: fields.StoreID,
		Name:    fields.Name,
		Address: fields.Address,
		City:    fields.City,
		State:   fields.State,
		ZipCode: fields.ZipCode,
		URL:     pageURL,
		Lat:     fields.Lat,
		Lon:     fields.Lon,
	}, nil
}

// collectHTMLSignals pulls the small set of page features the parser uses for
// store extraction: body text, title, first H1, first address block, script
// contents, and named meta tags. It is intentionally lossy so later parsing can
// use simple fallbacks without depending on the full DOM tree.
func collectHTMLSignals(body []byte) htmlSignals {
	signals := htmlSignals{
		Meta: make(map[string]string),
	}

	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		signals.BodyText = normalizeWhitespace(string(body))
		return signals
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script":
				signals.Scripts = append(signals.Scripts, scriptBlock{
					Type:    attrValue(n, "type"),
					Content: textContent(n),
				})
			case "meta":
				name := strings.ToLower(strings.TrimSpace(attrValue(n, "name")))
				if name == "" {
					name = strings.ToLower(strings.TrimSpace(attrValue(n, "property")))
				}
				content := strings.TrimSpace(attrValue(n, "content"))
				if name != "" && content != "" {
					signals.Meta[name] = content
				}
			case "title":
				if signals.Title == "" {
					signals.Title = normalizeWhitespace(textContent(n))
				}
			case "h1":
				if signals.H1 == "" {
					signals.H1 = normalizeWhitespace(textContent(n))
				}
			case "address":
				if signals.AddressText == "" {
					signals.AddressText = normalizeWhitespace(textContent(n))
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	signals.BodyText = normalizeWhitespace(textContent(doc))
	return signals
}

// extractFieldsFromJSONLD scans application/ld+json blocks and returns the
// strongest store-like candidate it can find. HEB pages may embed several JSON-LD
// objects, so this ranks candidates by how many store signals they contain
// instead of assuming the first object is the location record.
func extractFieldsFromJSONLD(scripts []scriptBlock) storeFields {
	best := storeFields{}
	for _, script := range scripts {
		if !strings.Contains(strings.ToLower(script.Type), "ld+json") {
			continue
		}

		var payload any
		if err := json.Unmarshal([]byte(script.Content), &payload); err != nil {
			continue
		}
		best = pickBetterFields(best, walkJSON(payload))
	}
	return best
}

func extractFieldsFromStructuredText(body []byte, signals htmlSignals) storeFields {
	fields := storeFields{
		Name:    firstMetaValue(signals.Meta, "og:title", "twitter:title", "title"),
		Address: firstMetaValue(signals.Meta, "business:contact_data:street_address", "og:street-address"),
		City:    firstMetaValue(signals.Meta, "business:contact_data:locality", "og:locality"),
		State:   firstMetaValue(signals.Meta, "business:contact_data:region", "og:region"),
		ZipCode: firstMetaValue(signals.Meta, "business:contact_data:postal_code", "og:postal-code"),
	}

	for _, candidate := range structuredTextCandidates(body, signals) {
		if fields.Address == "" {
			fields.Address = firstMatch(candidate, jsonStreetRe)
		}
		if fields.City == "" {
			fields.City = firstMatch(candidate, jsonCityRe)
		}
		if fields.State == "" {
			fields.State = strings.ToUpper(firstMatch(candidate, jsonStateRe))
		}
		if fields.ZipCode == "" {
			fields.ZipCode = firstMatch(candidate, jsonZipRe)
		}
		if fields.StoreID == "" {
			fields.StoreID = firstMatch(candidate, jsonStoreIDRe)
		}
		if fields.Address != "" && fields.City != "" && fields.State != "" && fields.ZipCode != "" && fields.StoreID != "" {
			break
		}
	}

	fields.Name = titleName(fields.Name)
	fields.Address = normalizeWhitespace(fields.Address)
	fields.City = normalizeWhitespace(fields.City)
	fields.State = strings.ToUpper(strings.TrimSpace(fields.State))
	fields.ZipCode = strings.TrimSpace(fields.ZipCode)
	fields.StoreID = strings.TrimSpace(fields.StoreID)
	fields.score = filledFieldCount(fields)
	return fields
}

func walkJSON(value any) storeFields {
	switch v := value.(type) {
	case map[string]any:
		best := fieldsFromObject(v)
		for _, child := range v {
			best = pickBetterFields(best, walkJSON(child))
		}
		return best
	case []any:
		best := storeFields{}
		for _, item := range v {
			best = pickBetterFields(best, walkJSON(item))
		}
		return best
	default:
		return storeFields{}
	}
}

func fieldsFromObject(obj map[string]any) storeFields {
	var fields storeFields
	fields.Name = stringValue(obj["name"])
	fields.StoreID = firstNumericString(
		obj["branchCode"],
		obj["storeCode"],
		obj["storeID"],
		obj["storeId"],
		obj["locationId"],
		obj["identifier"],
	)

	if address, ok := obj["address"].(map[string]any); ok {
		fields.Address = firstNonEmptyString(address["streetAddress"], address["line1"])
		fields.City = firstNonEmptyString(address["addressLocality"], address["city"])
		fields.State = firstNonEmptyString(address["addressRegion"], address["region"], address["state"])
		fields.ZipCode = firstNonEmptyString(address["postalCode"], address["zip"])
	}

	if geo, ok := obj["geo"].(map[string]any); ok {
		fields.Lat = numberPtr(geo["latitude"])
		fields.Lon = firstNonNilNumber(numberPtr(geo["longitude"]), numberPtr(geo["lng"]), numberPtr(geo["lon"]))
	}
	if fields.Lat == nil {
		fields.Lat = numberPtr(obj["latitude"])
	}
	if fields.Lon == nil {
		fields.Lon = firstNonNilNumber(numberPtr(obj["longitude"]), numberPtr(obj["lng"]), numberPtr(obj["lon"]))
	}

	score := 0
	if looksLikeStoreType(obj["@type"]) {
		score += 4
	}
	if fields.Name != "" {
		score++
	}
	if fields.Address != "" {
		score++
	}
	if fields.City != "" {
		score++
	}
	if fields.State != "" {
		score++
	}
	if fields.ZipCode != "" {
		score++
	}
	if fields.StoreID != "" {
		score++
	}
	if fields.Lat != nil && fields.Lon != nil {
		score += 2
	}
	fields.score = score

	return fields
}

func extractStoreID(text string) string {
	matches := corporateIDRe.FindStringSubmatch(text)
	if len(matches) == 2 {
		return matches[1]
	}
	return ""
}

func resolveStoreID(urlStoreID, explicitStoreID, extractedStoreID string) string {
	if explicitStoreID != "" {
		return explicitStoreID
	}
	if urlStoreID != "" {
		return urlStoreID
	}
	return extractedStoreID
}

func extractAddress(signals htmlSignals, pageCity string) (address, city, state, zip string) {
	normalizedCity := titleCaseSlug(pageCity)
	if normalizedCity != "" {
		cityExpr := regexp.MustCompile(`(?i)(.+?)\s+` + regexp.QuoteMeta(normalizedCity) + `,\s*([a-z]{2})\s+(\d{5}(?:-\d{4})?)`)
		for _, candidate := range []string{signals.AddressText, signals.Title, signals.BodyText} {
			matches := cityExpr.FindStringSubmatch(candidate)
			if len(matches) != 4 {
				continue
			}
			return normalizeWhitespace(matches[1]), normalizedCity, strings.ToUpper(matches[2]), matches[3]
		}
	}

	for _, candidate := range []string{signals.AddressText, signals.Title, signals.BodyText} {
		matches := addressLineRe.FindStringSubmatch(candidate)
		if len(matches) != 5 {
			continue
		}
		return normalizeWhitespace(matches[1]), normalizeWhitespace(matches[2]), strings.ToUpper(matches[3]), matches[4]
	}

	if signals.Title != "" {
		parts := strings.Split(signals.Title, "|")
		if len(parts) >= 2 {
			address = normalizeWhitespace(parts[1])
		}
	}
	return address, "", "", ""
}

func extractCoordinates(signals htmlSignals, body []byte) (*float64, *float64) {
	for _, key := range []string{"geo.position", "place:location:latitude"} {
		if value := strings.TrimSpace(signals.Meta[key]); value != "" {
			if key == "geo.position" {
				if lat, lon, ok := parseGeoPosition(value); ok {
					return lat, lon
				}
				continue
			}

			lat := parseFloat(strings.TrimSpace(value))
			lon := parseFloat(strings.TrimSpace(signals.Meta["place:location:longitude"]))
			if lat != nil && lon != nil {
				return lat, lon
			}
		}
	}

	bodyText := string(body)
	if matches := directionsCoordRe.FindStringSubmatch(bodyText); len(matches) == 3 {
		return parseFloat(matches[1]), parseFloat(matches[2])
	}

	latMatches := jsonLatitudeRe.FindStringSubmatch(bodyText)
	lonMatches := jsonLongitudeRe.FindStringSubmatch(bodyText)
	if len(latMatches) == 2 && len(lonMatches) == 2 {
		return parseFloat(latMatches[1]), parseFloat(lonMatches[1])
	}

	return nil, nil
}

func parseGeoPosition(value string) (*float64, *float64, bool) {
	matches := geoPositionMetaRe.FindStringSubmatch(value)
	if len(matches) != 3 {
		return nil, nil, false
	}
	return parseFloat(matches[1]), parseFloat(matches[2]), true
}

func titleName(title string) string {
	if title == "" {
		return ""
	}
	parts := strings.Split(title, "|")
	return normalizeWhitespace(parts[0])
}

func displayNameFromSlug(slug string) string {
	base := storePathIDRe.ReplaceAllString(slug, "")
	base = strings.Trim(base, "-")
	if base == "" {
		return ""
	}

	replacer := strings.NewReplacer(
		"h-e-b-plus", "H-E-B Plus",
		"h-e-b", "H-E-B",
		"mi-tienda", "Mi Tienda",
		"joe-v-s-smart-shop", "Joe V's Smart Shop",
	)
	base = replacer.Replace(base)

	parts := strings.Split(base, "-")
	for i, part := range parts {
		parts[i] = titleCaseToken(part)
	}
	name := normalizeWhitespace(strings.Join(parts, " "))
	name = strings.ReplaceAll(name, "H E B Plus", "H-E-B Plus")
	name = strings.ReplaceAll(name, "H E B", "H-E-B")
	name = strings.ReplaceAll(name, "Joe V S Smart Shop", "Joe V's Smart Shop")
	return name
}

func titleCaseSlug(slug string) string {
	parts := strings.Split(strings.TrimSpace(slug), "-")
	for i, part := range parts {
		parts[i] = titleCaseToken(part)
	}
	return normalizeWhitespace(strings.Join(parts, " "))
}

func titleCaseToken(part string) string {
	if part == "" {
		return ""
	}
	if isDigits(part) {
		return part
	}
	lower := strings.ToLower(part)
	return strings.ToUpper(lower[:1]) + lower[1:]
}

func fillMissingFields(base, extra storeFields) storeFields {
	if base.Name == "" {
		base.Name = extra.Name
	}
	if base.Address == "" {
		base.Address = extra.Address
	}
	if base.City == "" {
		base.City = extra.City
	}
	if base.State == "" {
		base.State = extra.State
	}
	if base.ZipCode == "" {
		base.ZipCode = extra.ZipCode
	}
	if base.StoreID == "" {
		base.StoreID = extra.StoreID
	}
	if base.Lat == nil {
		base.Lat = extra.Lat
	}
	if base.Lon == nil {
		base.Lon = extra.Lon
	}
	return base
}

func pickBetterFields(a, b storeFields) storeFields {
	if b.score > a.score {
		return b
	}
	if b.score < a.score {
		return a
	}
	if filledFieldCount(b) > filledFieldCount(a) {
		return b
	}
	return a
}

func filledFieldCount(f storeFields) int {
	count := 0
	for _, value := range []string{f.Name, f.Address, f.City, f.State, f.ZipCode, f.StoreID} {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	if f.Lat != nil && f.Lon != nil {
		count += 2
	}
	return count
}

func looksLikeStoreType(value any) bool {
	switch v := value.(type) {
	case string:
		return storeTypeName(v)
	case []any:
		for _, item := range v {
			if storeTypeName(stringValue(item)) {
				return true
			}
		}
	}
	return false
}

func storeTypeName(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, "store") || strings.Contains(value, "grocery")
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if s := stringValue(value); s != "" {
			return s
		}
	}
	return ""
}

func firstMetaValue(meta map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := normalizeWhitespace(meta[strings.ToLower(strings.TrimSpace(key))]); value != "" {
			return value
		}
	}
	return ""
}

func structuredTextCandidates(body []byte, signals htmlSignals) []string {
	seen := map[string]struct{}{}
	add := func(values *[]string, raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if _, ok := seen[raw]; ok {
			return
		}
		seen[raw] = struct{}{}
		*values = append(*values, raw)
	}

	candidates := make([]string, 0, len(signals.Scripts)+4)
	rawBody := string(body)
	unescapedBody := htmlstd.UnescapeString(strings.ReplaceAll(rawBody, `\"`, `"`))
	add(&candidates, rawBody)
	add(&candidates, strings.ReplaceAll(rawBody, `\"`, `"`))
	add(&candidates, htmlstd.UnescapeString(rawBody))
	add(&candidates, unescapedBody)
	for _, script := range signals.Scripts {
		add(&candidates, script.Content)
		add(&candidates, strings.ReplaceAll(script.Content, `\"`, `"`))
		add(&candidates, htmlstd.UnescapeString(script.Content))
	}
	return candidates
}

func firstMatch(value string, re *regexp.Regexp) string {
	matches := re.FindStringSubmatch(value)
	if len(matches) < 2 {
		return ""
	}
	return matches[1]
}

func firstNumericString(values ...any) string {
	for _, value := range values {
		if s := numericString(value); s != "" {
			return s
		}
	}
	return ""
}

func numericString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		v = strings.TrimSpace(v)
		if isDigits(v) {
			return v
		}
	case json.Number:
		return numericString(v.String())
	case float64:
		if math.Mod(v, 1) == 0 {
			return strconv.FormatInt(int64(v), 10)
		}
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case map[string]any:
		for _, key := range []string{"value", "@value", "id", "@id"} {
			if s := numericString(v[key]); s != "" {
				return s
			}
		}
	case []any:
		for _, item := range v {
			if s := numericString(item); s != "" {
				return s
			}
		}
	}
	return ""
}

func stringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case float64:
		if math.Mod(v, 1) == 0 {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}

func numberPtr(value any) *float64 {
	switch v := value.(type) {
	case nil:
		return nil
	case float64:
		f := v
		return &f
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return nil
		}
		return &f
	case string:
		return parseFloat(v)
	}
	return nil
}

func parseFloat(value string) *float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil
	}
	return &f
}

func firstNonNilNumber(values ...*float64) *float64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func attrValue(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if strings.EqualFold(attr.Key, key) {
			return attr.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
			b.WriteByte(' ')
		}
		if node.Type == html.ElementNode && (node.Data == "script" || node.Data == "style") {
			if node.Data == "script" {
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					if child.Type == html.TextNode {
						b.WriteString(child.Data)
					}
				}
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
