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
	User,
	Recipe,
	Location,
	Mail *template.Template

func init() {
	tmpls, err := template.ParseFS(htmlFiles, "*.html")
	if err != nil {
		panic(err.Error())
	}
	Home = ensure(tmpls, "home.html")
	Spin = ensure(tmpls, "spinner.html")
	User = ensure(tmpls, "user.html")
	Recipe = ensure(tmpls, "chat.html")
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

// basically a hack for uts till we make this non global
func SetClarity(project string) {
	clarityproject = project
}

var clarityproject string

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
