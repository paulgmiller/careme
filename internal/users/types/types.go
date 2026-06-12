package types

import (
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strings"
	"time"
)

type Recipe struct {
	Title     string    `json:"id"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID            string    `json:"id"`
	Email         []string  `json:"email"`
	CreatedAt     time.Time `json:"created_at"`
	LastRecipes   []Recipe  `json:"last_recipes,omitempty"`
	FavoriteStore string    `json:"favorite_store,omitempty"`
	ShoppingDay   string    `json:"shopping_day,omitempty"`
	MailOptIn     bool      `json:"mail_opt_in,omitempty"`
	Directive     string    `json:"directive,omitempty"`
}

// need to take a look up to location cache?
func (u User) Validate() error {
	if _, err := ParseWeekday(u.ShoppingDay); err != nil {
		return err
	}
	if len(u.Email) == 0 {
		return errors.New("at least one email is required")
	}
	for _, e := range u.Email {
		if _, err := mail.ParseAddress(e); err != nil {
			return errors.New("invalid email address: " + e)
		}
	}
	if u.FavoriteStore != "" {
		if !validFavoriteStoreID(u.FavoriteStore) {
			return fmt.Errorf("invalid favorite store id %s", u.FavoriteStore)
		}
	}
	// trim out recipes older than 2 months? store them in seperate file?
	slices.SortFunc(u.LastRecipes, func(a, b Recipe) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})

	return nil
}

func validFavoriteStoreID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '_' {
			continue
		}
		return false
	}
	return true
}

var daysOfWeek = [...]string{
	time.Sunday.String(),
	time.Monday.String(),
	time.Tuesday.String(),
	time.Wednesday.String(),
	time.Thursday.String(),
	time.Friday.String(),
	time.Saturday.String(),
}

func ParseWeekday(v string) (time.Weekday, error) {
	for i := range daysOfWeek {
		if strings.EqualFold(daysOfWeek[i], v) {
			return time.Weekday(i), nil
		}
	}

	return time.Saturday, fmt.Errorf("invalid weekday '%s'", v)
}
