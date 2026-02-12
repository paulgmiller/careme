package templates

import (
	"careme/internal/config"
	"embed"
	"html/template"
	"os"
)

//go:embed *.html
var htmlFiles embed.FS

var Home,
	Spin,
	AuthEstablish,
	User,
	ShoppingList,
	Recipe,
	Location,
	Mail *template.Template

func Init(config *config.Config, tailwindhash string) error {
	funcs := template.FuncMap{
		"ClerkEnabled":        func() bool { return config.Clerk.PublishableKey != "" },
		"ClerkPublishableKey": func() string { return config.Clerk.PublishableKey },
		"TailwindAssetPath":   func() string { return "/static/tailwind-" + tailwindhash + ".css" },
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
	Location = ensure(tmpls, "locations.html")
	Mail = ensure(tmpls, "mail.html")

	//todo pull from config.
	Clarityproject = os.Getenv("CLARITY_PROJECT_ID")
	return nil
}

func ensure(templates *template.Template, name string) *template.Template {
	tmpl := templates.Lookup(name)
	if tmpl == nil {
		panic("template " + name + " not found")
	}
	return tmpl
}

var Clarityproject string

// ClarityScript generates the Microsoft Clarity tracking script HTML
func ClarityScript() template.HTML {
	if Clarityproject == "" {
		return ""
	}

	script := `<script type="text/javascript">
    (function(c,l,a,r,i,t,y){
        c[a]=c[a]||function(){(c[a].q=c[a].q||[]).push(arguments)};
        t=l.createElement(r);t.async=1;t.src="https://www.clarity.ms/tag/"+i;
        y=l.getElementsByTagName(r)[0];y.parentNode.insertBefore(t,y);
    })(window, document, "clarity", "script", "` + Clarityproject + `");
</script>`

	return template.HTML(script)
}
