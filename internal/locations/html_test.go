package locations

import (
	"bytes"
	"careme/internal/config"
	"testing"

	"golang.org/x/net/html"
)

func isValidHTML(t *testing.T, htmlStr string) {
	if htmlStr == "" {
		t.Fatal("rendered HTML is empty")
	}
	_, err := html.Parse(bytes.NewBufferString(htmlStr))
	if err != nil {
		t.Fatalf("rendered HTML is not valid: %v\nHTML:\n%s", err, htmlStr)
	}
}

func TestLocationsHtml_ValidHTML(t *testing.T) {
	cfg := &config.Config{
		Clarity: config.ClarityConfig{ProjectID: "test123"},
	}
	locs := []Location{
		{ID: "L1", Name: "Store One", Address: "100 Main St"},
		{ID: "L2", Name: "Store Two", Address: "200 Oak Ave"},
	}
	html := Html(cfg, nil, locs, "12345")
	isValidHTML(t, html)
}

func TestLocationsHtml_IncludesClarityScript(t *testing.T) {
	cfg := &config.Config{
		Clarity: config.ClarityConfig{ProjectID: "test123"},
	}
	locs := []Location{
		{ID: "L1", Name: "Store One", Address: "100 Main St"},
	}
	html := Html(cfg, nil, locs, "12345")

	if !bytes.Contains([]byte(html), []byte("www.clarity.ms/tag/")) {
		t.Error("HTML should contain Clarity script URL")
	}

	if !bytes.Contains([]byte(html), []byte("test123")) {
		t.Error("HTML should contain project ID")
	}
}

func TestLocationsHtml_NoClarityWhenEmpty(t *testing.T) {
	cfg := &config.Config{
		Clarity: config.ClarityConfig{ProjectID: ""},
	}
	locs := []Location{
		{ID: "L1", Name: "Store One", Address: "100 Main St"},
	}
	html := Html(cfg, nil, locs, "12345")

	if bytes.Contains([]byte(html), []byte("clarity.ms")) {
		t.Error("HTML should not contain Clarity script when project ID is empty")
	}
}

type testUser struct {
	favoriteStore string
}

func (u testUser) GetFavoriteStore() string {
	return u.favoriteStore
}

func TestLocationsHtml_WithFavoriteStore(t *testing.T) {
	cfg := &config.Config{
		Clarity: config.ClarityConfig{ProjectID: "test123"},
	}
	locs := []Location{
		{ID: "L1", Name: "Store One", Address: "100 Main St"},
		{ID: "L2", Name: "Store Two", Address: "200 Oak Ave"},
	}
	user := testUser{favoriteStore: "L1"}
	html := Html(cfg, user, locs, "12345")

	if !bytes.Contains([]byte(html), []byte("Set as Favorite")) {
		t.Error("HTML should contain favorite button text")
	}

	if !bytes.Contains([]byte(html), []byte("★ Favorite")) {
		t.Error("HTML should contain active favorite indicator")
	}

	if !bytes.Contains([]byte(html), []byte("/user/favorite-store")) {
		t.Error("HTML should contain favorite store endpoint")
	}
}

func TestLocationsHtml_WithoutUser(t *testing.T) {
	cfg := &config.Config{
		Clarity: config.ClarityConfig{ProjectID: "test123"},
	}
	locs := []Location{
		{ID: "L1", Name: "Store One", Address: "100 Main St"},
		{ID: "L2", Name: "Store Two", Address: "200 Oak Ave"},
	}
	html := Html(cfg, nil, locs, "12345")

	if bytes.Contains([]byte(html), []byte("Set as Favorite")) {
		t.Error("HTML should not contain favorite button when user is not logged in")
	}

	if bytes.Contains([]byte(html), []byte("/user/favorite-store")) {
		t.Error("HTML should not contain favorite store endpoint when user is not logged in")
	}
}
