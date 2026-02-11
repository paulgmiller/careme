package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/sitemap"
	"careme/internal/users"

	"golang.org/x/net/html"
)

func TestWebEndToEndFlowWithMocks(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	client := newTestClient(t)
	resp := mustGet(t, client, srv.URL+"/ready") //our readiness probe works even with mocks?
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected /ready to return 200 OK, got %d", resp.StatusCode)
	}

	// Step 1: query locations for 90005 and ensure it returns a /recipes?location link.
	locationsBody := mustGetBody(t, client, srv.URL+"/locations?zip=90005")
	locationID := extractLocationID(t, locationsBody)

	// Step 2: go to /recipes?location=<id> and follow redirects until recipes render.
	initialRecipesURL := srv.URL + "/recipes?location=" + url.QueryEscape(locationID)
	_, recipesBody := followUntilRecipes(t, client, initialRecipesURL, true /*expectSpinner*/)

	// Step 3: select one recipe to save and two to dismiss.
	conversationID := extractHiddenValue(t, recipesBody, "conversation_id")
	date := extractHiddenValue(t, recipesBody, "date")
	location := extractHiddenValue(t, recipesBody, "location")
	recipeHashes := extractRecipeHashes(t, recipesBody)
	if len(recipeHashes) < 3 {
		t.Fatalf("expected at least 3 recipes, got %d", len(recipeHashes))
	}

	savedHash := recipeHashes[0]
	// Step 2b: load a shared recipe page directly.
	_ = mustGetBody(t, client, srv.URL+"/recipe/"+url.PathEscape(savedHash))
	dismissedHashes := recipeHashes[1:3]

	//step 4 todo  regenrate again with commentary then save two more

	// Step 5: finalize with the saved/dismissed selections.
	finalizeURL := buildRecipesURL(srv.URL, location, date, conversationID, savedHash, dismissedHashes, true)
	_, finalizedBody := followUntilRecipes(t, client, finalizeURL, false /*expectSpinner*/)
	recipeHashes = extractRecipeHashes(t, finalizedBody)
	if len(recipeHashes) != 1 {
		t.Fatalf("expected finalized page to show 1 recipe, got %d", len(recipeHashes))
	}
	if recipeHashes[0] != savedHash {
		t.Fatalf("expected finalized recipe to be %s, got %s", savedHash, recipeHashes[0])
	}
	for _, dismissed := range dismissedHashes {
		if recipeHashes[0] == dismissed {
			t.Fatalf("finalized recipe %s was in dismissed list", dismissed)
		}
	}

	// Step 6: ask a question on the finalized single recipe page.
	question := "Can I use skirt steak instead?"
	questionURL := srv.URL + "/recipe/" + url.PathEscape(savedHash) + "/question"
	questionBody := mustPostFormBody(t, client, questionURL, url.Values{
		"conversation_id": {conversationID},
		"question":        {question},
	})
	if !strings.Contains(questionBody, question) {
		t.Fatalf("expected question thread to include question %q", question)
	}
	if !strings.Contains(questionBody, "Mock answer: "+question) {
		t.Fatalf("expected question thread to include mock answer for %q", question)
	}

	//TODO step 6 make sure recipes are saved to user page?

}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	cfg := &config.Config{Mocks: config.MockConfig{Enable: true}}
	cacheDir := filepath.Join(t.TempDir(), "cache")
	cacheStore := cache.NewFileCache(cacheDir)
	userStorage := users.NewStorage(cacheStore)

	generator, err := recipes.NewGenerator(cfg, cacheStore)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}
	locationStorage, err := locations.New(cfg)
	if err != nil {
		t.Fatalf("failed to create location server: %v", err)
	}

	mockAuth := auth.Mock(cfg)

	mux := http.NewServeMux()
	locationServer := locations.NewServer(locationStorage, userStorage)
	locationServer.Register(mux, mockAuth)
	users.NewHandler(userStorage, locationStorage, mockAuth).Register(mux)
	recipes.NewHandler(cfg, userStorage, generator, locationStorage, cacheStore, mockAuth, sitemap.New()).Register(mux)

	ro := &readyOnce{}
	ro.Add(generator, locationServer)

	mux.Handle("/ready", ro)

	return httptest.NewServer(WithMiddleware(mux))
}

func newTestClient(t *testing.T) *http.Client {
	return &http.Client{}
}

func mustGet(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s failed: %v", url, err)
	}
	return resp
}

func mustGetBody(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp := mustGet(t, client, url)
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("failed to close response body: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body := readAll(t, resp.Body)
		t.Fatalf("GET %s expected 200, got %d: %s", url, resp.StatusCode, body)
	}
	body := readAll(t, resp.Body)
	requireValidHTML(t, url, resp.Header.Get("Content-Type"), body)
	return body
}

func mustPostFormBody(t *testing.T, client *http.Client, targetURL string, data url.Values) string {
	t.Helper()
	resp, err := client.PostForm(targetURL, data)
	if err != nil {
		t.Fatalf("POST %s failed: %v", targetURL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("failed to close response body: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body := readAll(t, resp.Body)
		t.Fatalf("POST %s expected 200 after redirect, got %d: %s", targetURL, resp.StatusCode, body)
	}
	body := readAll(t, resp.Body)
	requireValidHTML(t, targetURL, resp.Header.Get("Content-Type"), body)
	return body
}

func followUntilRecipes(t *testing.T, client *http.Client, startURL string, expectSpinner bool) (string, string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	current := startURL
	sawSpinner := false
	for {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for recipes page starting at %s", startURL)
		}

		resp := mustGet(t, client, current)

		body := readAll(t, resp.Body)
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("failed to close response body: %v", err)
		}

		if isSpinner(body) {
			sawSpinner = true
			time.Sleep(100 * time.Millisecond)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected recipes page 200, got %d: %s", resp.StatusCode, body)
		}

		if sawSpinner != expectSpinner {
			t.Fatal("expected spinner but never got one")
		}

		return current, body
	}
}

func isSpinner(body string) bool {
	return strings.Contains(body, "<title>Generating") || strings.Contains(body, "Please wait")
}

func extractLocationID(t *testing.T, body string) string {
	t.Helper()
	re := regexp.MustCompile(`href="/recipes\?location=([^"]+)"`)
	match := re.FindStringSubmatch(body)
	if len(match) < 2 {
		t.Fatalf("expected locations page to include /recipes?location link")
	}
	return match[1]
}

func extractHiddenValue(t *testing.T, body, name string) string {
	t.Helper()
	re := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `" value="([^"]*)"`)
	match := re.FindStringSubmatch(body)
	if len(match) < 2 {
		t.Fatalf("expected hidden input %q in page", name)
	}
	return match[1]
}

func extractRecipeHashes(t *testing.T, body string) []string {
	t.Helper()
	re := regexp.MustCompile(`id="save-([^"]+)"`)
	matches := re.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		t.Fatalf("expected recipe save inputs in page")
	}
	seen := make(map[string]struct{})
	var hashes []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if _, ok := seen[match[1]]; ok {
			continue
		}
		seen[match[1]] = struct{}{}
		hashes = append(hashes, match[1])
	}
	return hashes
}

func buildRecipesURL(base, location, date, conversationID, savedHash string, dismissedHashes []string, finalize bool) string {
	params := url.Values{}
	params.Set("location", location)
	params.Set("date", date)
	params.Set("conversation_id", conversationID)
	params.Add("saved", savedHash)
	for _, hash := range dismissedHashes {
		params.Add("dismissed", hash)
	}
	if finalize {
		params.Set("finalize", "true")
	}
	return base + "/recipes?" + params.Encode()
}

func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return string(data)
}

func requireValidHTML(t *testing.T, url, contentType, body string) {
	t.Helper()
	if strings.TrimSpace(body) == "" {
		t.Fatalf("GET %s returned empty body", url)
	}
	if contentType != "" && !strings.Contains(strings.ToLower(contentType), "text/html") {
		t.Fatalf("GET %s expected HTML content-type, got %q", url, contentType)
	}
	if !strings.Contains(strings.ToLower(body), "<html") {
		t.Fatalf("GET %s expected HTML body, missing <html> tag", url)
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("GET %s returned invalid HTML: %v", url, err)
	}
	if !hasElement(doc, "body") {
		t.Fatalf("GET %s expected HTML body element", url)
	}
}

func hasElement(n *html.Node, name string) bool {
	if n.Type == html.ElementNode && n.Data == name {
		return true
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if hasElement(child, name) {
			return true
		}
	}
	return false
}
