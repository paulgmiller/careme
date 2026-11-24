package updates

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"time"

	"careme/internal/cache"
)

type Media struct {
	Src  string `json:"src"`
	Type string `json:"type"`
	Alt  string `json:"alt"`
}

type Post struct {
	ID    string    `json:"id"`
	Text  string    `json:"text"`
	Date  time.Time `json:"date"`
	Media []Media   `json:"media"`
}

var validID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func ValidID(id string) bool { return validID.MatchString(id) }

func ReadPosts(ctx context.Context, store cache.Cache) ([]Post, error) {
	reader, err := store.Get(ctx, Prefix+"feed.json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	var posts []Post
	err = json.NewDecoder(reader).Decode(&posts)
	return posts, err
}

func SortPosts(posts []Post) {
	sort.Slice(posts, func(i, j int) bool {
		if posts[i].Date.Equal(posts[j].Date) {
			return posts[i].ID > posts[j].ID
		}
		return posts[i].Date.After(posts[j].Date)
	})
}
