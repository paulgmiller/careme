package demo

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"
	"slices"

	"careme/internal/guest"
	"careme/internal/routing"
)

//go:embed demo.html
var demoHTML string
var demoTemplate = template.Must(template.New("demo").Parse(demoHTML))

type section struct {
	Name  string
	Items []string
}

// Register serves the frozen source inventory without grading or external calls.
func Register(mux routing.Registrar) {
	mux.HandleFunc("GET /demo/mnfood", func(w http.ResponseWriter, r *http.Request) {
		guest.EnsureShoppingListCount(w, r)
		var sections []section
		for _, name := range []string{"Seasonal", "Small Seasonal", "Low Carb", "Staple", "Fruit", "Small Fruit", "Fruit substitution", "Recommended add-on"} {
			group := section{Name: name}
			for _, item := range catalog() {
				if slices.Contains(item.Categories, name) {
					group.Items = append(group.Items, item.Description)
				}
			}
			sections = append(sections, group)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = demoTemplate.Execute(w, struct {
			LocationID string
			Sections   []section
		}{LocationID: LocationID, Sections: sections})
	})
	mux.HandleFunc("GET /demo/mnfood/ingredients", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(catalog())
	})
}
