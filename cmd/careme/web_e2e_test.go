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
	"careme/internal/templates"
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
	savedTitle := extractRecipeTitleForHash(t, finalizedBody, savedHash)
	for _, dismissed := range dismissedHashes {
		if recipeHashes[0] == dismissed {
			t.Fatalf("finalized recipe %s was in dismissed list", dismissed)
		}
	}

	// Step 6: ask a question on the finalized single recipe page.
	question := "Can I use skirt steak instead?"
	questionURL := srv.URL + "/recipe/" + url.PathEscape(savedHash) + "/question"
	questionBody := mustPostFormBodyHTMX(t, client, questionURL, url.Values{
		"conversation_id": {conversationID},
		"question":        {question},
		"recipe_title":    {savedTitle},
	})
	if !strings.Contains(questionBody, question) {
		t.Fatalf("expected question thread to include question %q", question)
	}
	expectedPrompt := "Regarding " + savedTitle + ": " + question
	if !strings.Contains(questionBody, "Mock answer: "+expectedPrompt) {
		t.Fatalf("expected question thread to include mock answer for %q", expectedPrompt)
	}

	// Step 7: submit cooked feedback and ensure it persists on the recipe page.
	feedbackURL := srv.URL + "/recipe/" + url.PathEscape(savedHash) + "/feedback"
	feedbackComment := "Turned out great. Next time add more lime."
	feedbackBody := mustPostFormBodyHTMX(t, client, feedbackURL, url.Values{
		"cooked":   {"true"},
		"stars":    {"4"},
		"feedback": {feedbackComment},
	})
	if !strings.Contains(feedbackBody, "Saved") {
		t.Fatalf("expected feedback response to include saved confirmation, got: %s", feedbackBody)
	}

	recipeBody := mustGetBody(t, client, srv.URL+"/recipe/"+url.PathEscape(savedHash))
	if !strings.Contains(recipeBody, feedbackComment) {
		t.Fatalf("expected recipe page to contain saved feedback comment %q", feedbackComment)
	}
	if !strings.Contains(recipeBody, `data-initial-stars="4"`) {
		t.Fatalf("expected recipe page to persist stars value, got body: %s", recipeBody)
	}

	//TODO step 6 make sure recipes are saved to user page?

}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	cfg := &config.Config{Mocks: config.MockConfig{Enable: true}}
	err := templates.Init(cfg, "dummyhash") //initialize templates so they don't hit the file system during tests
	if err != nil {
		t.Fatalf("failed to create templates %v", err)
	}

	cacheDir := filepath.Join(t.TempDir(), "cache")
	cacheStore := cache.NewFileCache(cacheDir)
	userStorage := users.NewStorage(cacheStore)

	generator, err := recipes.NewGenerator(cfg, cacheStore)
	if err != nil {
		t.Fatalf("failed to create generator: %v", err)
	}
	locationStorage, err := locations.New(cfg, cacheStore)
	if err != nil {
		t.Fatalf("failed to create location server: %v", err)
	}

	mockAuth := auth.Mock(cfg)

	mux := http.NewServeMux()
	locationServer := locations.NewServer(locationStorage, userStorage)
	locationServer.Register(mux, mockAuth)
	users.NewHandler(userStorage, locationStorage, mockAuth).Register(mux)
	recipes.NewHandler(cfg, userStorage, generator, locationStorage, cacheStore, mockAuth).Register(mux)

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

func mustPostFormBodyHTMX(t *testing.T, client *http.Client, targetURL string, data url.Values) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, targetURL, strings.NewReader(data.Encode()))
	if err != nil {
		t.Fatalf("POST %s failed to build request: %v", targetURL, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	resp, err := client.Do(req)
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
		t.Fatalf("POST %s expected 200, got %d: %s", targetURL, resp.StatusCode, body)
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

func extractRecipeTitleForHash(t *testing.T, body, hash string) string {
	t.Helper()
	re := regexp.MustCompile(`<a href="/recipe/` + regexp.QuoteMeta(hash) + `"[^>]*>\s*([^<]+)\s*</a>`)
	match := re.FindStringSubmatch(body)
	if len(match) < 2 {
		t.Fatalf("expected finalized page to include title link for hash %q", hash)
	}
	return strings.TrimSpace(match[1])
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

type trackedRequest struct {
	method       string
	url          string
	duration     time.Duration
	responseCode string
}

type fakeRequestTracker struct {
	calls []trackedRequest
}

func (f *fakeRequestTracker) TrackRequest(method, url string, duration time.Duration, responseCode string) {
	f.calls = append(f.calls, trackedRequest{
		method:       method,
		url:          url,
		duration:     duration,
		responseCode: responseCode,
	})
}

func TestAppInsightsTrackerTracksResponseCode(t *testing.T) {
	tracker := &fakeRequestTracker{}
	mw := &appInsightsTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}),
		tracker: tracker,
	}

	req := httptest.NewRequest(http.MethodPost, "https://careme.cooking/recipes?vegan=true", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if len(tracker.calls) != 1 {
		t.Fatalf("expected 1 tracked request, got %d", len(tracker.calls))
	}

	call := tracker.calls[0]
	if call.method != http.MethodPost {
		t.Fatalf("expected method %q, got %q", http.MethodPost, call.method)
	}
	if call.url != req.URL.String() {
		t.Fatalf("expected url %q, got %q", req.URL.String(), call.url)
	}
	if call.responseCode != "201" {
		t.Fatalf("expected response code 201, got %q", call.responseCode)
	}
	if call.duration <= 0 {
		t.Fatalf("expected positive duration, got %s", call.duration)
	}
}

func TestAppInsightsTrackerDefaultsStatusCodeTo200(t *testing.T) {
	tracker := &fakeRequestTracker{}
	mw := &appInsightsTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}),
		tracker: tracker,
	}

	req := httptest.NewRequest(http.MethodGet, "https://careme.cooking/about", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if len(tracker.calls) != 1 {
		t.Fatalf("expected 1 tracked request, got %d", len(tracker.calls))
	}
	if tracker.calls[0].responseCode != "200" {
		t.Fatalf("expected response code 200, got %q", tracker.calls[0].responseCode)
	}
}

func TestAppInsightsTrackerSkipsReady(t *testing.T) {
	tracker := &fakeRequestTracker{}
	mw := &appInsightsTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		tracker: tracker,
	}

	req := httptest.NewRequest(http.MethodGet, "https://careme.cooking/ready", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if len(tracker.calls) != 0 {
		t.Fatalf("expected 0 tracked requests for /ready, got %d", len(tracker.calls))
	}
}

func TestAppInsightsTrackerTracksRecoveredPanicAs500(t *testing.T) {
	tracker := &fakeRequestTracker{}
	mw := &appInsightsTracker{
		Handler: &recoverer{
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic("boom")
			}),
		},
		tracker: tracker,
	}

	req := httptest.NewRequest(http.MethodGet, "https://careme.cooking/panic", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
	if len(tracker.calls) != 1 {
		t.Fatalf("expected 1 tracked request, got %d", len(tracker.calls))
	}
	if tracker.calls[0].responseCode != "500" {
		t.Fatalf("expected response code 500, got %q", tracker.calls[0].responseCode)
	}
}

func TestParseAppInsightsConnectionString(t *testing.T) {
	connectionString := "InstrumentationKey=test-key;IngestionEndpoint=https://westus3-1.in.applicationinsights.azure.com/;LiveEndpoint=https://westus3.livediagnostics.monitor.azure.com/;ApplicationId=app-id"
	params, err := parseAppInsightsConnectionString(connectionString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.instrumentationKey != "test-key" {
		t.Fatalf("expected instrumentation key test-key, got %q", params.instrumentationKey)
	}
	if params.ingestionEndpoint.String() != "https://westus3-1.in.applicationinsights.azure.com/" {
		t.Fatalf("unexpected ingestion endpoint: %q", params.ingestionEndpoint.String())
	}
}

func TestParseAppInsightsConnectionStringErrors(t *testing.T) {
	testCases := []struct {
		name        string
		value       string
		wantErrText string
	}{
		{
			name:        "empty",
			value:       "",
			wantErrText: "connection string is empty",
		},
		{
			name:        "missing instrumentation key",
			value:       "IngestionEndpoint=https://westus3-1.in.applicationinsights.azure.com/",
			wantErrText: "instrumentation key is missing",
		},
		{
			name:        "missing ingestion endpoint",
			value:       "InstrumentationKey=test-key",
			wantErrText: "ingestion endpoint is missing",
		},
		{
			name:        "bad ingestion endpoint",
			value:       "InstrumentationKey=test-key;IngestionEndpoint=:bad://",
			wantErrText: "missing protocol scheme",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAppInsightsConnectionString(tc.value)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErrText)
			}
			if !strings.Contains(err.Error(), tc.wantErrText) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErrText, err.Error())
			}
		})
	}
}
