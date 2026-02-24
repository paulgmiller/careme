package users

import (
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"
)

type adminUserView struct {
	ID      string
	Emails  []string
	Recipes []string
}

var adminUsersPageTmpl = template.Must(template.New("admin-users").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Admin Users</title>
</head>
<body>
  <h1>Users</h1>
  <p>Total users: {{len .Users}}</p>
  <table border="1" cellpadding="6" cellspacing="0">
    <thead>
      <tr>
        <th>User ID</th>
        <th>Emails</th>
        <th>Saved Recipes</th>
      </tr>
    </thead>
    <tbody>
      {{range .Users}}
      <tr>
        <td>{{.ID}}</td>
        <td>
          {{if .Emails}}
          <ul>
            {{range .Emails}}
            <li>{{.}}</li>
            {{end}}
          </ul>
          {{else}}
          none
          {{end}}
        </td>
        <td>
          {{if .Recipes}}
          <ul>
            {{range .Recipes}}
            <li>{{.}}</li>
            {{end}}
          </ul>
          {{else}}
          none
          {{end}}
        </td>
      </tr>
      {{end}}
    </tbody>
  </table>
</body>
</html>`))

func AdminUsersPage(storage *Storage) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		list, err := storage.List(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to list users for admin page", "error", err)
			http.Error(w, "unable to load users", http.StatusInternalServerError)
			return
		}

		views := make([]adminUserView, 0, len(list))
		for _, user := range list {
			recipeTitles := make([]string, 0, len(user.LastRecipes))
			for _, recipe := range user.LastRecipes {
				title := strings.TrimSpace(recipe.Title)
				if title == "" {
					continue
				}
				recipeTitles = append(recipeTitles, title)
			}

			views = append(views, adminUserView{
				ID:      user.ID,
				Emails:  append([]string(nil), user.Email...),
				Recipes: recipeTitles,
			})
		}

		sort.Slice(views, func(i, j int) bool {
			iEmail := primaryAdminEmail(views[i])
			jEmail := primaryAdminEmail(views[j])
			if iEmail == jEmail {
				return views[i].ID < views[j].ID
			}
			return iEmail < jEmail
		})

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if err := adminUsersPageTmpl.Execute(w, struct {
			Users []adminUserView
		}{Users: views}); err != nil {
			slog.ErrorContext(r.Context(), "failed to render admin users page", "error", err)
			http.Error(w, "unable to render users", http.StatusInternalServerError)
			return
		}
	})
}

func primaryAdminEmail(v adminUserView) string {
	if len(v.Emails) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(v.Emails[0]))
}
