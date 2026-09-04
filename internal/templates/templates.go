package templates

import (
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/url"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"careme/internal/config"
	"careme/internal/logsetup"
	"careme/internal/static"
)

const clerkJSVersion = "5.99.0"

const (
	shoppingRecipeImageWidth = 480
	emailRecipeImageWidth    = 752
	recipeImageQuality       = 75
)

// ConversionEvent identifies a neutral browser conversion published through Google Tag Manager.
type ConversionEvent string

const (
	SignupCompletedConversion  ConversionEvent = "signup_completed"
	RecipeGenerationConversion ConversionEvent = "recipe_generation"
	RecipeSaveConversion       ConversionEvent = "recipe_save"
)

//go:embed *.html
var htmlFiles embed.FS

var Home,
	Spin,
	AuthEstablish,
	AccountRequired,
	User,
	ShoppingList,
	Recipe,
	Critique,
	About,
	Privacy,
	TemperatureGuide,
	CocktailLocations,
	Cocktails,
	Location,
	FarmersMarket,
	Mail *template.Template

func Init(config *config.Config) error {
	publicOrigin := config.ResolvedPublicOrigin()
	funcs := template.FuncMap{
		"ClerkEnabled":        func() bool { return config.Clerk.PublishableKey != "" },
		"ClerkPublishableKey": func() string { return config.Clerk.PublishableKey },
		"ClerkJSVersion":      func() string { return clerkJSVersion },
		"ClerkUIBundleURL": func() string {
			domain := strings.TrimSpace(config.Clerk.Domain)
			if domain == "" {
				return ""
			}
			return "https://" + domain + "/npm/@clerk/ui@1/dist/ui.browser.js"
		},
		"GoogleTagNoScript":         GoogleTagNoScript,
		"EmailRecipeImageURL":       emailRecipeImageURL,
		"InstructionNumber":         func(index int) int { return index + 1 },
		"PublicOrigin":              func() string { return publicOrigin },
		"RecipeSaveConversion":      func() ConversionEvent { return RecipeSaveConversion },
		"SignInPath":                signInPath,
		"SignupCompletedConversion": func() ConversionEvent { return SignupCompletedConversion },
		"ShoppingRecipeImageURL":    func(hash string) string { return shoppingRecipeImageURL(publicOrigin, hash) },
		"AssetPath":                 func() string { return static.AssetPath },
		"UserInitial":               userInitial,
	}
	tmpls, err := template.New("all").Funcs(funcs).ParseFS(htmlFiles, "*.html")
	if err != nil {
		return err
	}
	Home = ensure(tmpls, "home.html")
	Spin = ensure(tmpls, "spinner.html")
	AuthEstablish = ensure(tmpls, "auth_establish.html")
	AccountRequired = ensure(tmpls, "account_required.html")
	User = ensure(tmpls, "user.html")
	ShoppingList = ensure(tmpls, "shoppinglist.html")
	Recipe = ensure(tmpls, "recipe.html")
	Critique = ensure(tmpls, "critique.html")
	About = ensure(tmpls, "about.html")
	Privacy = ensure(tmpls, "privacy.html")
	TemperatureGuide = ensure(tmpls, "temperature_guide.html")
	CocktailLocations = ensure(tmpls, "cocktail_locations.html")
	Cocktails = ensure(tmpls, "cocktails.html")
	Location = ensure(tmpls, "locations.html")
	FarmersMarket = ensure(tmpls, "farmersmarket.html")
	Mail = ensure(tmpls, "mail.html")

	// todo pull from config.
	Clarityproject = os.Getenv("CLARITY_PROJECT_ID")
	GoogleTagManagerID = os.Getenv("GOOGLE_TAG_MANAGER_ID")
	return nil
}

func shoppingRecipeImageURL(publicOrigin, hash string) string {
	return recipeImagePath(publicOrigin, hash, shoppingRecipeImageWidth, "auto")
}

func emailRecipeImageURL(publicOrigin, hash string) string {
	return strings.TrimRight(publicOrigin, "/") + recipeImagePath(publicOrigin, hash, emailRecipeImageWidth, "jpeg")
}

func recipeImagePath(publicOrigin, hash string, width int, imageFormat string) string {
	originalPath := "/recipe/" + hash + "/image"
	if !supportsCloudflareImageTransformations(publicOrigin) {
		return originalPath
	}

	return fmt.Sprintf(
		"/cdn-cgi/image/width=%d,quality=%d,format=%s,onerror=redirect%s",
		width,
		recipeImageQuality,
		imageFormat,
		originalPath,
	)
}

func supportsCloudflareImageTransformations(publicOrigin string) bool {
	parsed, err := url.Parse(strings.TrimSpace(publicOrigin))
	if err != nil {
		return false
	}

	hostname := strings.ToLower(parsed.Hostname())
	return hostname == "careme.cooking" || strings.HasSuffix(hostname, ".careme.cooking")
}

func ensure(templates *template.Template, name string) *template.Template {
	tmpl := templates.Lookup(name)
	if tmpl == nil {
		panic("template " + name + " not found")
	}
	return tmpl
}

func signInPath(returnTo string) string {
	returnTo = strings.TrimSpace(returnTo)
	if returnTo == "" {
		return "/sign-in"
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(returnTo))
	return "/sign-in?return_to_b64=" + url.QueryEscape(encoded)
}

func userInitial(userEmail []string) string {
	for _, email := range userEmail {
		trimmed := strings.TrimSpace(email)
		if trimmed == "" {
			continue
		}
		r, _ := utf8.DecodeRuneInString(trimmed)
		if r == utf8.RuneError {
			return "?"
		}
		return string(unicode.ToUpper(r))
	}
	return "?"
}

var (
	Clarityproject     string
	GoogleTagManagerID string
)

// ClarityScript generates the Microsoft Clarity tracking script HTML.
func ClarityScript(ctx context.Context) template.HTML {
	if Clarityproject == "" {
		return ""
	}
	sessionID, _ := logsetup.SessionIDFromContext(ctx)

	script := `<script type="text/javascript">
    (function(c,l,a,r,i,t,y){
        c[a]=c[a]||function(){(c[a].q=c[a].q||[]).push(arguments)};
        t=l.createElement(r);t.async=1;t.src="https://www.clarity.ms/tag/"+i;
        y=l.getElementsByTagName(r)[0];y.parentNode.insertBefore(t,y);
    })(window, document, "clarity", "script", "` + Clarityproject + `");
`
	if sessionID != "" {
		script += `
    window.clarity("identify", "` + template.JSEscapeString(sessionID) + `", "` + template.JSEscapeString(sessionID) + `");
`
	}
	script += `
</script>`

	return template.HTML(script)
}

// GoogleTagScript generates the Google Tag Manager snippet HTML.
func GoogleTagScript() template.HTML {
	if GoogleTagManagerID == "" {
		return ""
	}

	script := `<!-- Google Tag Manager -->
<script>
  (function(w,d,s,l,i){w[l]=w[l]||[];w[l].push({'gtm.start':
  new Date().getTime(),event:'gtm.js'});var f=d.getElementsByTagName(s)[0],
  j=d.createElement(s),dl=l!='dataLayer'?'&l='+l:'';j.async=true;j.src=
  'https://www.googletagmanager.com/gtm.js?id='+i+dl;f.parentNode.insertBefore(j,f);
  })(window,document,'script','dataLayer','` + template.JSEscapeString(GoogleTagManagerID) + `');
</script>
<!-- End Google Tag Manager -->`

	return template.HTML(script)
}

// GoogleTagNoScript generates the Google Tag Manager noscript fallback HTML.
func GoogleTagNoScript() template.HTML {
	if GoogleTagManagerID == "" {
		return ""
	}

	iframe := `<!-- Google Tag Manager (noscript) -->
<noscript><iframe src="https://www.googletagmanager.com/ns.html?id=` + template.HTMLEscapeString(GoogleTagManagerID) + `" height="0" width="0" style="display:none;visibility:hidden"></iframe></noscript>
<!-- End Google Tag Manager (noscript) -->`

	return template.HTML(iframe)
}
