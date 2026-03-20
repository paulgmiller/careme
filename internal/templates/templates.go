package templates

import (
	"context"
	"embed"
	"html/template"
	"os"

	"careme/internal/config"
	"careme/internal/logsetup"
)

//go:embed *.html
var htmlFiles embed.FS

var Home,
	Spin,
	AuthEstablish,
	User,
	ShoppingList,
	Recipe,
	About,
	Location,
	Mail *template.Template

func Init(config *config.Config, tailwindAssetPath string) error {
	funcs := template.FuncMap{
		"ClerkEnabled":        func() bool { return config.Clerk.PublishableKey != "" },
		"ClerkPublishableKey": func() string { return config.Clerk.PublishableKey },
		"PublicOrigin":        func() string { return config.ResolvedPublicOrigin() },
		"TailwindAssetPath":   func() string { return tailwindAssetPath },
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
	About = ensure(tmpls, "about.html")
	Location = ensure(tmpls, "locations.html")
	Mail = ensure(tmpls, "mail.html")

	// todo pull from config.
	Clarityproject = os.Getenv("CLARITY_PROJECT_ID")
	GoogleTagID = os.Getenv("GOOGLE_TAG_ID")
	GoogleConversionLabel = os.Getenv("GOOGLE_CONVERSION_LABEL")
	return nil
}

func ensure(templates *template.Template, name string) *template.Template {
	tmpl := templates.Lookup(name)
	if tmpl == nil {
		panic("template " + name + " not found")
	}
	return tmpl
}

var (
	Clarityproject        string
	GoogleTagID           string
	GoogleConversionLabel string
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

// GoogleTagScript generates the Google tag snippet HTML.
func GoogleTagScript() template.HTML {
	if GoogleTagID == "" {
		return ""
	}

	script := `<script async src="https://www.googletagmanager.com/gtag/js?id=` + GoogleTagID + `"></script>
<script>
  window.dataLayer = window.dataLayer || [];
  function gtag(){dataLayer.push(arguments);}
  gtag('js', new Date());

  gtag('config', '` + GoogleTagID + `');
</script>`

	return template.HTML(script)
}

func GoogleConversionTag() string {
	if GoogleTagID == "" || GoogleConversionLabel == "" {
		return ""
	}
	return GoogleTagID + "/" + GoogleConversionLabel
}
