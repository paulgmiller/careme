package templates

import (
	"bytes"
	"context"
	"html/template"
	"io/fs"
	"strings"
	"testing"

	"careme/internal/config"
	"careme/internal/logsetup"
	"careme/internal/seasons"
	utypes "careme/internal/users/types"

	"golang.org/x/net/html"
)

func TestClarityScriptIncludesSessionID(t *testing.T) {
	prev := Clarityproject
	t.Cleanup(func() {
		Clarityproject = prev
	})
	Clarityproject = "proj-123"

	script := string(ClarityScript(logsetup.WithSessionID(context.Background(), "sess-123")))
	if !strings.Contains(script, `www.clarity.ms/tag/`) {
		t.Fatal("expected clarity script url")
	}
	if !strings.Contains(script, `window.clarity("identify", "sess-123", "sess-123")`) {
		t.Fatalf("expected identify call in script, got %q", script)
	}
}

func TestClarityScriptOmitsIdentifyWhenSessionIDEmpty(t *testing.T) {
	prev := Clarityproject
	t.Cleanup(func() {
		Clarityproject = prev
	})
	Clarityproject = "proj-123"

	script := string(ClarityScript(context.Background()))
	if strings.Contains(script, `window.clarity("identify"`) {
		t.Fatalf("did not expect identify call in script, got %q", script)
	}
}

func TestGoogleTagNoScriptIncludesContainerID(t *testing.T) {
	prev := GoogleTagManagerID
	t.Cleanup(func() {
		GoogleTagManagerID = prev
	})
	GoogleTagManagerID = "GTM-KP55TPW6"

	script := string(GoogleTagNoScript())
	if !strings.Contains(script, `www.googletagmanager.com/ns.html?id=GTM-KP55TPW6`) {
		t.Fatalf("expected GTM noscript iframe URL, got %q", script)
	}
	if !strings.Contains(script, `<!-- Google Tag Manager (noscript) -->`) {
		t.Fatalf("expected GTM noscript comments, got %q", script)
	}
}

func TestFullPageTemplatesIncludeSeasonalBackground(t *testing.T) {
	for _, name := range []string{
		"about.html",
		"critique.html",
		"farmersmarket.html",
		"home.html",
		"locations.html",
		"recipe.html",
		"shoppinglist.html",
		"spinner.html",
		"user.html",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := htmlFiles.ReadFile(name)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			if !strings.Contains(string(body), `{{template "seasonal_background" .}}`) {
				t.Fatalf("%s should include seasonal background", name)
			}
			doc, err := html.Parse(strings.NewReader(string(body)))
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			mainClasses, ok := firstElementClasses(doc, "main")
			if !ok {
				t.Fatalf("%s should include a main element", name)
			}
			for _, class := range []string{"relative", "px-4"} {
				if !mainClasses[class] {
					t.Fatalf("%s should keep page content in a relative main container with class %q", name, class)
				}
			}
			if name != "user.html" && !mainClasses["z-10"] {
				t.Fatalf("%s should layer page content above the seasonal background", name)
			}
		})
	}
}

func TestBrowserPageTemplatesIncludeAppHead(t *testing.T) {
	nonAppPages := map[string]bool{
		"auth_establish.html": true,
		"mail.html":           true,
	}

	names, err := fs.Glob(htmlFiles, "*.html")
	if err != nil {
		t.Fatalf("glob templates: %v", err)
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			body, err := htmlFiles.ReadFile(name)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			rendered := string(body)
			if !strings.Contains(rendered, "<head") || nonAppPages[name] {
				return
			}
			if !strings.Contains(rendered, `{{template "app_head" .Style}}`) {
				t.Fatalf("%s should include app_head for PWA metadata", name)
			}
		})
	}
}

func firstElementClasses(node *html.Node, element string) (map[string]bool, bool) {
	if node.Type == html.ElementNode && node.Data == element {
		classes := make(map[string]bool)
		for _, attr := range node.Attr {
			if attr.Key != "class" {
				continue
			}
			for _, class := range strings.Fields(attr.Val) {
				classes[class] = true
			}
			return classes, true
		}
		return classes, true
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if classes, ok := firstElementClasses(child, element); ok {
			return classes, true
		}
	}
	return nil, false
}

func TestTemplatePageTitlesAreUnique(t *testing.T) {
	titles := make(map[string]string)
	for _, name := range []string{
		"about.html",
		"auth_establish.html",
		"critique.html",
		"farmersmarket.html",
		"home.html",
		"locations.html",
		"mail.html",
		"recipe.html",
		"shoppinglist.html",
		"spinner.html",
		"user.html",
	} {
		body, err := htmlFiles.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		title, ok := templateTitle(string(body))
		if !ok {
			t.Fatalf("%s should include a title", name)
		}
		if previous, exists := titles[title]; exists {
			t.Fatalf("%s and %s should not share title %q", previous, name, title)
		}
		titles[title] = name
	}
}

func templateTitle(body string) (string, bool) {
	start := strings.Index(body, "<title>")
	if start == -1 {
		return "", false
	}
	start += len("<title>")
	end := strings.Index(body[start:], "</title>")
	if end == -1 {
		return "", false
	}
	return strings.TrimSpace(body[start : start+end]), true
}

func TestAboutTemplateRendersValidHTML(t *testing.T) {
	if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	data := NewAboutPageData(context.Background(), seasons.GetCurrentStyle())

	var buf bytes.Buffer
	if err := About.Execute(&buf, data); err != nil {
		t.Fatalf("About.Execute() error = %v", err)
	}

	rendered := buf.String()
	if rendered == "" {
		t.Fatal("about page rendered empty HTML")
	}
	if !strings.Contains(rendered, `<meta name="description" content="Learn how Careme helps home cooks find fresh nearby ingredients, plan practical recipes, and cook more often." />`) {
		t.Fatalf("about page should include meta description, body: %s", rendered)
	}
	if _, err := html.Parse(strings.NewReader(rendered)); err != nil {
		t.Fatalf("about page rendered invalid HTML: %v\nHTML:\n%s", err, rendered)
	}
	for _, sectionID := range []string{`id="album"`, `id="ethos"`, `id="follow"`, `id="faq"`, `id="github"`} {
		if !strings.Contains(rendered, sectionID) {
			t.Fatalf("about page should include %s section, body: %s", sectionID, rendered)
		}
	}
	for _, heading := range []string{">Album</h2>", "Ethos", ">Follow Careme</h2>", ">FAQ</h2>", ">GitHub</h2>"} {
		if !strings.Contains(rendered, heading) {
			t.Fatalf("about page should include %q heading, body: %s", heading, rendered)
		}
	}
	for _, link := range []string{
		"https://github.com/paulgmiller/careme/issues/472",
		"https://www.facebook.com/careme.cooking",
		"https://bsky.app/profile/northbriton.net",
		"https://github.com/paulgmiller/careme",
	} {
		if !strings.Contains(rendered, link) {
			t.Fatalf("about page should include %q link, body: %s", link, rendered)
		}
	}
	for _, label := range []string{`aria-label="Facebook"`, `aria-label="Instagram coming soon"`, `aria-label="Bluesky"`} {
		if !strings.Contains(rendered, label) {
			t.Fatalf("about page should include %s social label, body: %s", label, rendered)
		}
	}
	if strings.Contains(rendered, `id="privacy"`) {
		t.Fatalf("about page should not include old privacy section, body: %s", rendered)
	}
	for _, oldHeading := range []string{"1. Album", "2. Ethos", "3. Follow Careme", "4. FAQ", "5. GitHub"} {
		if strings.Contains(rendered, oldHeading) {
			t.Fatalf("about page should not include numbered heading %q, body: %s", oldHeading, rendered)
		}
	}
	if got := strings.Count(rendered, `data-full="`); got != len(data.AlbumPhotos) {
		t.Fatalf("about page should render %d album photos, got %d", len(data.AlbumPhotos), got)
	}
	wantCaptions := 0
	for _, photo := range data.AlbumPhotos {
		if photo.RecipeHash != "" {
			wantCaptions++
		}
	}
	if got := strings.Count(rendered, `data-caption='`); got != wantCaptions {
		t.Fatalf("about page should render %d recipe captions, got %d", wantCaptions, got)
	}
	if !strings.Contains(rendered, "Dungeness crab pasta") {
		t.Fatalf("about page should render album comments from Go data, body: %s", rendered)
	}
}

func TestSpinTemplateIncludesClerkRefreshWhenEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Clerk.PublishableKey = "pk_test_123"
	if err := Init(cfg, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() {
		if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
			t.Fatalf("cleanup Init() error = %v", err)
		}
	})

	data := struct {
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Style           seasons.Style
		ServerSignedIn  bool
		RefreshInterval string
		StatusMessage   string
	}{
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  false,
		RefreshInterval: "10",
		StatusMessage:   "Ingredients are ready. Building your recipes.",
	}

	var buf bytes.Buffer
	if err := Spin.Execute(&buf, data); err != nil {
		t.Fatalf("Spin.Execute() error = %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, `data-clerk-publishable-key="pk_test_123"`) {
		t.Fatalf("spinner page should include Clerk bootstrap script, body: %s", rendered)
	}
	if !strings.Contains(rendered, `const serverSignedIn =`) || !strings.Contains(rendered, `!serverSignedIn && clerkSignedIn`) {
		t.Fatalf("spinner page should pass server sign-in state to Clerk refresh logic, body: %s", rendered)
	}
	if !strings.Contains(rendered, data.StatusMessage) {
		t.Fatalf("spinner page should render status message, body: %s", rendered)
	}
}

func TestFarmersMarketTemplateRendersWithoutErrorField(t *testing.T) {
	cfg := &config.Config{}
	cfg.Clerk.PublishableKey = "pk_test_123"
	if err := Init(cfg, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() {
		if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
			t.Fatalf("cleanup Init() error = %v", err)
		}
	})

	data := struct {
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Style           seasons.Style
		ServerSignedIn  bool
	}{
		Style:          seasons.GetCurrentStyle(),
		ServerSignedIn: true,
	}

	var buf bytes.Buffer
	if err := FarmersMarket.Execute(&buf, data); err != nil {
		t.Fatalf("FarmersMarket.Execute() error = %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, "Farmers market finds") {
		t.Fatalf("farmers market page should render title, body: %s", rendered)
	}
	if strings.Contains(rendered, `.Error`) {
		t.Fatalf("farmers market page should not reference an Error field, body: %s", rendered)
	}
	if !strings.Contains(rendered, `const serverSignedIn =`) || !strings.Contains(rendered, `true`) {
		t.Fatalf("farmers market page should pass server sign-in state to Clerk refresh logic, body: %s", rendered)
	}
}

func TestClerkJSScriptsUsePinnedVersion(t *testing.T) {
	for _, name := range []string{"auth_establish.html", "clerk_refresh.html"} {
		t.Run(name, func(t *testing.T) {
			body, err := htmlFiles.ReadFile(name)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			rendered := string(body)
			if strings.Contains(rendered, "@latest") {
				t.Fatalf("%s should not use @latest for ClerkJS", name)
			}
			if !strings.Contains(rendered, "@{{ClerkJSVersion}}") {
				t.Fatalf("%s should use pinned ClerkJS template helper", name)
			}
		})
	}

	if clerkJSVersion == "" {
		t.Fatal("clerkJSVersion should be pinned")
	}
}

func TestUserTemplateLoadsClerkBillingScriptWhenEnabled(t *testing.T) {
	cfg := &config.Config{}
	cfg.Clerk.PublishableKey = "pk_test_123"
	cfg.Clerk.Domain = "clerk.example.com"
	if err := Init(cfg, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() {
		if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
			t.Fatalf("cleanup Init() error = %v", err)
		}
	})

	data := struct {
		ClarityScript     template.HTML
		GoogleTagScript   template.HTML
		Style             seasons.Style
		User              *utypes.User
		Success           bool
		FavoriteStoreName string
		ActiveTab         string
		PastRecipes       []utypes.Recipe
		ServerSignedIn    bool
	}{
		Style:          seasons.GetCurrentStyle(),
		User:           &utypes.User{Email: []string{"chef@example.com"}},
		ActiveTab:      "customize",
		ServerSignedIn: true,
	}

	var buf bytes.Buffer
	if err := User.Execute(&buf, data); err != nil {
		t.Fatalf("User.Execute() error = %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, `data-clerk-pricing-table data-clerk-ui-bundle-url="https://clerk.example.com/npm/@clerk/ui@1/dist/ui.browser.js"`) {
		t.Fatalf("user page should pass Clerk UI bundle URL to billing script, body: %s", rendered)
	}
	if !strings.Contains(rendered, `<script src="/static/user-clerk-billing.js"></script>`) {
		t.Fatalf("user page should load Clerk billing script asset, body: %s", rendered)
	}
	if strings.Contains(rendered, `mountPricingTable`) {
		t.Fatalf("user page should not inline Clerk pricing table mount logic, body: %s", rendered)
	}
}

func TestSpinTemplatePreservesStatusLineBreaks(t *testing.T) {
	if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	data := struct {
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Style           seasons.Style
		ServerSignedIn  bool
		RefreshInterval string
		StatusMessage   string
	}{
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  false,
		RefreshInterval: "10",
		StatusMessage:   "Considering ingredients\nHalf Off Spinach",
	}

	var buf bytes.Buffer
	if err := Spin.Execute(&buf, data); err != nil {
		t.Fatalf("Spin.Execute() error = %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, "whitespace-pre-line") {
		t.Fatalf("spinner status should render line breaks with CSS, body: %s", rendered)
	}
	if !strings.Contains(rendered, data.StatusMessage) {
		t.Fatalf("spinner status should keep newline text, body: %s", rendered)
	}
}

func TestFarmersMarketTemplateUsesHTMXUpload(t *testing.T) {
	if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	data := struct {
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Style           seasons.Style
		ServerSignedIn  bool
		Error           string
	}{
		Style: seasons.GetCurrentStyle(),
	}

	var buf bytes.Buffer
	if err := FarmersMarket.Execute(&buf, data); err != nil {
		t.Fatalf("FarmersMarket.Execute() error = %v", err)
	}

	rendered := buf.String()
	for _, want := range []string{
		`<script src="/static/htmx@2.0.8.js"></script>`,
		`id="farmers-market-error"`,
		`hx-post="/farmersmarket"`,
		`hx-encoding="multipart/form-data"`,
		`hx-target="#farmers-market-work"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("farmers market page should include %q, body: %s", want, rendered)
		}
	}
}

func TestHomeTemplateRendersFavoriteStoreChefNotes(t *testing.T) {
	if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	data := struct {
		ClarityScript     template.HTML
		GoogleTagScript   template.HTML
		User              *utypes.User
		FavoriteStoreName string
		Style             seasons.Style
		ServerSignedIn    bool
	}{
		User: &utypes.User{
			Email:         []string{"chef@example.com"},
			FavoriteStore: "70500874",
		},
		FavoriteStoreName: "Ballard Market",
		Style:             seasons.GetCurrentStyle(),
		ServerSignedIn:    true,
	}

	var buf bytes.Buffer
	if err := Home.Execute(&buf, data); err != nil {
		t.Fatalf("Home.Execute() error = %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, `<meta name="description" content="Careme helps you find nearby stores and cook fresh, seasonal recipes with ingredients that are close to home." />`) {
		t.Fatalf("home page should include meta description, body: %s", rendered)
	}
	if !strings.Contains(rendered, "Ballard Market") {
		t.Fatalf("home page should render favorite store name, body: %s", rendered)
	}
	if !strings.Contains(rendered, `id="favorite-store-chef-notes"`) {
		t.Fatalf("home page should render favorite store chef notes toggle control, body: %s", rendered)
	}
	if !strings.Contains(rendered, "Chef notes") {
		t.Fatalf("home page should render chef notes toggle, body: %s", rendered)
	}
	if !strings.Contains(rendered, `name="instructions"`) {
		t.Fatalf("home page should render instructions textarea, body: %s", rendered)
	}
	if !strings.Contains(rendered, `/recipes?location=70500874`) {
		t.Fatalf("home page should render direct recipe link, body: %s", rendered)
	}
}

func TestHomeTemplateOmitsFavoriteStoreChefNotesWithoutFavoriteStore(t *testing.T) {
	if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	data := struct {
		ClarityScript     template.HTML
		GoogleTagScript   template.HTML
		User              *utypes.User
		FavoriteStoreName string
		Style             seasons.Style
		ServerSignedIn    bool
	}{
		User: &utypes.User{
			Email: []string{"chef@example.com"},
		},
		Style:          seasons.GetCurrentStyle(),
		ServerSignedIn: true,
	}

	var buf bytes.Buffer
	if err := Home.Execute(&buf, data); err != nil {
		t.Fatalf("Home.Execute() error = %v", err)
	}

	rendered := buf.String()
	if strings.Contains(rendered, `id="favorite-store-chef-notes"`) {
		t.Fatalf("home page should not render favorite store chef notes toggle without a favorite store, body: %s", rendered)
	}
	if strings.Contains(rendered, `for="favorite-store-chef-notes"`) {
		t.Fatalf("home page should not render favorite store chef notes label without a favorite store, body: %s", rendered)
	}
	if strings.Contains(rendered, "Chef notes") {
		t.Fatalf("home page should not render favorite store chef notes copy without a favorite store, body: %s", rendered)
	}
	if strings.Contains(rendered, `name="instructions"`) {
		t.Fatalf("home page should not render favorite store instructions field without a favorite store, body: %s", rendered)
	}
}

func TestHomeTemplateIncludesPWAMetadata(t *testing.T) {
	if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	style := seasons.GetCurrentStyle()
	data := struct {
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Style           seasons.Style
		User            *utypes.User
		ServerSignedIn  bool
	}{
		Style: style,
	}

	var buf bytes.Buffer
	if err := Home.Execute(&buf, data); err != nil {
		t.Fatalf("Home.Execute() error = %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, `<link rel="manifest" href="/manifest.webmanifest">`) {
		t.Fatalf("home page should include manifest link, body: %s", rendered)
	}
	if !strings.Contains(rendered, `<meta name="theme-color" content="`+style.Colors.C50+`">`) {
		t.Fatalf("home page should use the page background color for PWA chrome, body: %s", rendered)
	}
	if !strings.Contains(rendered, `<link rel="apple-touch-icon" href="/static/app-icon-192.png">`) {
		t.Fatalf("home page should include app icon link, body: %s", rendered)
	}
	if !strings.Contains(rendered, `navigator.serviceWorker.register("/sw.js")`) {
		t.Fatalf("home page should register the service worker, body: %s", rendered)
	}
	if !strings.Contains(rendered, `CAREME_SYNC_SAVED_RECIPES`) {
		t.Fatalf("home page should tell the service worker to sync saved recipes, body: %s", rendered)
	}
	if !strings.Contains(rendered, `caremeSavedRecipesSynced`) {
		t.Fatalf("home page should avoid syncing saved recipes on every page load, body: %s", rendered)
	}
	if !strings.Contains(rendered, `syncSavedRecipesForOffline().then((synced) =>`) {
		t.Fatalf("home page should wait for saved recipe sync before marking it complete, body: %s", rendered)
	}
	if !strings.Contains(rendered, `if (synced) sessionStorage.setItem("caremeSavedRecipesSynced", "1");`) {
		t.Fatalf("home page should set saved recipe sync flag only after success, body: %s", rendered)
	}
	if !strings.Contains(rendered, `careme:saved-recipes-changed`) {
		t.Fatalf("home page should refresh offline saved recipes after save changes, body: %s", rendered)
	}
}

func TestAuthEstablishTemplateChecksUserExistenceBeforeRedirect(t *testing.T) {
	cfg := &config.Config{}
	cfg.Clerk.PublishableKey = "pk_test_123"
	if err := Init(cfg, "dummyhash.css"); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() {
		if err := Init(&config.Config{}, "dummyhash.css"); err != nil {
			t.Fatalf("cleanup Init() error = %v", err)
		}
	})

	data := struct {
		PublishableKey  string
		GoogleTagScript template.HTML
		UserExistsURL   string
		ReturnTo        string
	}{
		PublishableKey: "pk_test_123",
		UserExistsURL:  "/auth/user-exists",
		ReturnTo:       "/recipe/hash",
	}

	var buf bytes.Buffer
	if err := AuthEstablish.Execute(&buf, data); err != nil {
		t.Fatalf("AuthEstablish.Execute() error = %v", err)
	}

	rendered := buf.String()
	if !strings.Contains(rendered, `const returnTo = "\/recipe\/hash" || "/";`) {
		t.Fatalf("auth establish page should inline return target, body: %s", rendered)
	}
	if !strings.Contains(rendered, `const userExistsURL = "\/auth\/user-exists";`) {
		t.Fatalf("auth establish page should inline user exists endpoint, body: %s", rendered)
	}
	if !strings.Contains(rendered, `fetch(userExistsURL, {`) {
		t.Fatalf("auth establish page should call user exists endpoint, body: %s", rendered)
	}
	if strings.Contains(rendered, `for (let attempt = 0; attempt < 5; attempt++)`) {
		t.Fatalf("auth establish page should not retry user exists check, body: %s", rendered)
	}
	if !strings.Contains(rendered, `if (!payload.exists &&`) {
		t.Fatalf("auth establish page should gate conversion on missing user, body: %s", rendered)
	}
	if !strings.Contains(rendered, `event: "signup_completed"`) {
		t.Fatalf("auth establish page should push signup event to dataLayer, body: %s", rendered)
	}
	if !strings.Contains(rendered, `eventCallback: finishRedirect`) {
		t.Fatalf("auth establish page should redirect after GTM event callback, body: %s", rendered)
	}
	if !strings.Contains(rendered, "console.warn(`auth user exists failed: ${response.status}`)") {
		t.Fatalf("auth establish page should log when user exists endpoint returns a failure, body: %s", rendered)
	}
	if strings.Contains(rendered, `.catch((error) => {`) {
		t.Fatalf("auth establish page should not use a catch handler for user exists failures, body: %s", rendered)
	}
}
