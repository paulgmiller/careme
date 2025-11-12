package templates

import (
	"embed"
	"html/template"
)

//go:embed *.html
var htmlFiles embed.FS

var (
	Home,
	Spin,
	User,
	Recipe,
	Location *template.Template
)

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
}

func ensure(templates *template.Template, name string) *template.Template {
	tmpl := templates.Lookup(name)
	if tmpl == nil {
		panic("template " + name + " not found")
	}
	return tmpl
}
