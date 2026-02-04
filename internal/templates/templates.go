package templates

import (
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

func init() {
	clerkPublishableKey = os.Getenv("CLERK_PUBLISHABLE_KEY")
	funcs := template.FuncMap{
		"ClerkEnabled":        ClerkEnabled,
		"ClerkPublishableKey": ClerkPublishableKey,
	}
	tmpls, err := template.New("all").Funcs(funcs).ParseFS(htmlFiles, "*.html")
	if err != nil {
		panic(err.Error())
	}
	Home = ensure(tmpls, "home.html")
	Spin = ensure(tmpls, "spinner.html")
	AuthEstablish = ensure(tmpls, "auth_establish.html")
	User = ensure(tmpls, "user.html")
	ShoppingList = ensure(tmpls, "shoppinglist.html")
	Recipe = ensure(tmpls, "recipe.html")
	Location = ensure(tmpls, "locations.html")
	Mail = ensure(tmpls, "mail.html")

	clarityproject = os.Getenv("CLARITY_PROJECT_ID")
}

func ensure(templates *template.Template, name string) *template.Template {
	tmpl := templates.Lookup(name)
	if tmpl == nil {
		panic("template " + name + " not found")
	}
	return tmpl
}

// basically a hack for us till we make this non global
func SetClarity(project string) {
	clarityproject = project
}

var clarityproject string
var clerkPublishableKey string

// ClarityScript generates the Microsoft Clarity tracking script HTML
func ClarityScript() template.HTML {
	if clarityproject == "" {
		return ""
	}

	script := `<script type="text/javascript">
    (function(c,l,a,r,i,t,y){
        c[a]=c[a]||function(){(c[a].q=c[a].q||[]).push(arguments)};
        t=l.createElement(r);t.async=1;t.src="https://www.clarity.ms/tag/"+i;
        y=l.getElementsByTagName(r)[0];y.parentNode.insertBefore(t,y);
    })(window, document, "clarity", "script", "` + clarityproject + `");
</script>`

	return template.HTML(script)
}

// ClerkEnabled reports whether Clerk is configured for templates.
func ClerkEnabled() bool {
	return clerkPublishableKey != ""
}

// ClerkPublishableKey returns the Clerk publishable key for templates.
func ClerkPublishableKey() string {
	return clerkPublishableKey
}
