package users

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"careme/internal/cache"
	"careme/internal/recipes/feedback"

	utypes "careme/internal/users/types"
)

type adminUserView struct {
	ID                string
	Emails            []string
	SavedRecipeCount  int
	CookedRecipeCount int
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
        <th>Saved Recipe Count</th>
        <th>Cooked Click Count</th>
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
          {{.SavedRecipeCount}}
        </td>
        <td>
          {{.CookedRecipeCount}}
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
		filtered := filterAdminUsers(list)

		if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "emails") {
			renderAdminEmailsText(w, filtered)
			return
		}

		views := make([]adminUserView, 0, len(filtered))
		for _, user := range filtered {
			views = append(views, adminUserView{
				ID:                user.ID,
				Emails:            append([]string(nil), user.Email...),
				SavedRecipeCount:  len(user.LastRecipes),
				CookedRecipeCount: cookedRecipeCount(r.Context(), storage.cache, user),
			})
		}

		sort.Slice(views, func(i, j int) bool {
			if views[i].SavedRecipeCount == views[j].SavedRecipeCount {
				iEmail := primaryAdminEmail(views[i])
				jEmail := primaryAdminEmail(views[j])
				if iEmail == jEmail {
					return views[i].ID < views[j].ID
				}
				return iEmail < jEmail
			}
			return views[i].SavedRecipeCount > views[j].SavedRecipeCount
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

func filterAdminUsers(users []utypes.User) []utypes.User {
	filtered := make([]utypes.User, 0, len(users))
	for _, user := range users {
		if !strings.HasPrefix(user.ID, "user_") {
			continue
		}
		filtered = append(filtered, user)
	}
	return filtered
}

func cookedRecipeCount(ctx context.Context, c cache.Cache, user utypes.User) int {
	hashes := make([]string, 0, len(user.LastRecipes))
	for _, recipe := range user.LastRecipes {
		if time.Since(recipe.CreatedAt) > 30*time.Hour*24 {
			continue
		}
		hashes = append(hashes, recipe.Hash)
	}
	return len(feedback.NewIO(c).CookedHashes(ctx, hashes))
}

func renderAdminEmailsText(w http.ResponseWriter, users []utypes.User) {
	unique := make(map[string]struct{})
	for _, user := range users {
		for _, email := range user.Email {
			normalized := strings.ToLower(strings.TrimSpace(email))
			if normalized == "" {
				continue
			}
			unique[normalized] = struct{}{}
		}
	}

	emails := make([]string, 0, len(unique))
	for email := range unique {
		emails = append(emails, email)
	}
	sort.Strings(emails)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	for _, email := range emails {
		_, _ = w.Write([]byte(email + "\n"))
	}
}
