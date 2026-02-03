package main

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/users"
)

func TestWebEndToEndFlowWithMocks(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	client := newTestClient(t)

	// Step 1: query locations for 90005 and ensure it returns a /recipes?location link.
	locationsBody := mustGetBody(t, client, srv.URL+"/locations?zip=90005")
	locationID := extractLocationID(t, locationsBody)

	// Log in to avoid redirect back to home when hitting /recipes.
	login(t, client, srv.URL, "test@example.com")

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

	//TODO step 6 make sure recipes are saved to user page?

}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	ctx := t.Context()

	cfg := &config.Config{Mocks: config.MockConfig{Enable: true}}
	cacheDir := filepath.Join(t.TempDir(), "cache")
	cacheStore := cache.NewFileCache(cacheDir)
	userStorage := users.NewStorage(cacheStore)

	generator, err := recipes.NewGenerator(cfg, cacheStore)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}
	locationServer, err := locations.New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create location server: %v", err)
	}

	mux := http.NewServeMux()
	locations.Register(locationServer, mux)
	users.NewHandler(userStorage, locationServer).Register(mux)
	recipes.NewHandler(cfg, userStorage, generator, locationServer, cacheStore).Register(mux)

	//todo find a better way to mock this or move it to web.go?
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form submission", http.StatusBadRequest)
			return
		}
		email := strings.TrimSpace(r.FormValue("email"))
		if email == "" {
			http.Error(w, "email is required", http.StatusBadRequest)
			return
		}
		user, err := userStorage.FindOrCreateByEmail(email)
		if err != nil {
			http.Error(w, "unable to sign in", http.StatusInternalServerError)
			return
		}
		users.SetCookie(w, user.ID, sessionDuration)
		w.WriteHeader(http.StatusOK)
	})

	return httptest.NewServer(WithMiddleware(mux))
}

func newTestClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("failed to create cookie jar: %v", err)
	}
	return &http.Client{
		Jar: jar,
	}
}

func login(t *testing.T, client *http.Client, baseURL, email string) {
	t.Helper()
	form := url.Values{}
	form.Set("email", email)
	resp, err := client.PostForm(baseURL+"/login", form)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("failed to close login response body: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		body := readAll(t, resp.Body)
		t.Fatalf("expected login 200, got %d: %s", resp.StatusCode, body)
	}
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
	return readAll(t, resp.Body)
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
