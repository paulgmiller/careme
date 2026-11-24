package updates

import (
	"bytes"
	_ "embed"
	"html/template"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
)

//go:embed post.html
var postHTML string

var (
	postTemplate = template.Must(template.New("post").Parse(postHTML))
	urlPattern   = regexp.MustCompile(`https?://[^\s]+`)
)

type (
	textPart  struct{ Text, Href string }
	imageView struct {
		Media
		Wide bool
	}
)
type postView struct {
	Post            Post
	Parts           []textPart
	Images          []imageView
	Videos          []Media
	Newer, Older    string
	Position, Total int
}

func servePost(w http.ResponseWriter, r *http.Request, posts []Post) {
	SortPosts(posts)
	view := postView{Total: len(posts)}
	index := 0
	if id := r.URL.Query().Get("id"); id != "" {
		index = -1
		for i, p := range posts {
			if p.ID == id {
				index = i
				break
			}
		}
		if index < 0 {
			http.NotFound(w, r)
			return
		}
	}
	if len(posts) > 0 {
		view.Post = posts[index]
		view.Position = index + 1
		if index > 0 {
			view.Newer = posts[index-1].ID
		}
		if index+1 < len(posts) {
			view.Older = posts[index+1].ID
		}
		text := view.Post.Text
		offset := 0
		for _, match := range urlPattern.FindAllStringIndex(text, -1) {
			view.Parts = append(view.Parts, textPart{Text: text[offset:match[0]]}, textPart{Text: text[match[0]:match[1]], Href: text[match[0]:match[1]]})
			offset = match[1]
		}
		view.Parts = append(view.Parts, textPart{Text: text[offset:]})
		for _, item := range view.Post.Media {
			if strings.HasPrefix(item.Type, "image/") {
				view.Images = append(view.Images, imageView{Media: item})
			}
			if strings.HasPrefix(item.Type, "video/") {
				view.Videos = append(view.Videos, item)
			}
		}
		if len(view.Images)%2 == 1 {
			view.Images[len(view.Images)-1].Wide = true
		}
	}
	var body bytes.Buffer
	if err := postTemplate.Execute(&body, view); err != nil {
		slog.ErrorContext(r.Context(), "render published update", "error", err)
		http.Error(w, "could not render update", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	if r.Method != http.MethodHead {
		_, _ = w.Write(body.Bytes())
	}
}
