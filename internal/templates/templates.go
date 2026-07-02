package templates

import (
	"context"
	"embed"
	"encoding/base64"
	"html/template"
	"net/url"
	"os"
	"strings"

	"careme/internal/config"
	"careme/internal/logsetup"
)

const clerkJSVersion = "5.99.0"

//go:embed *.html
var htmlFiles embed.FS

var Home,
	Spin,
	AuthEstablish,
	User,
	ShoppingList,
	Recipe,
	Critique,
	About,
	Location,
	FarmersMarket,
	Mail *template.Template

func Init(config *config.Config, tailwindAssetPath, fontsAssetPath string) error {
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
		"GoogleTagNoScript": GoogleTagNoScript,
		"PublicOrigin":      func() string { return config.ResolvedPublicOrigin() },
		"SignInPath":        signInPath,
		"TailwindAssetPath": func() string { return tailwindAssetPath },
		"FontsAssetPath":    func() string { return fontsAssetPath },
	}
	tmpls, err := template.New("all").Funcs(funcs).ParseFS(htmlFiles, "*.html")
	if err != nil {
		return err
	}
	Home = ensure(tmpls, "home.html")
	Spin = ensure(tmpls, "spinner.html")
	AuthEstablish = ensure(tmpls, "auth_establish.html")
	User = ensure(tmpls, "user.html")
	ShoppingList = ensure(tmpls, "shoppinglist.html")
	Recipe = ensure(tmpls, "recipe.html")
	Critique = ensure(tmpls, "critique.html")
	About = ensure(tmpls, "about.html")
	Location = ensure(tmpls, "locations.html")
	FarmersMarket = ensure(tmpls, "farmersmarket.html")
	Mail = ensure(tmpls, "mail.html")

	// todo pull from config.
	Clarityproject = os.Getenv("CLARITY_PROJECT_ID")
	GoogleTagManagerID = os.Getenv("GOOGLE_TAG_MANAGER_ID")
	return nil
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
