package types

import (
	"errors"
	"fmt"
	"net/mail"
	"slices"
	"strconv"
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
}

// need to take a look up to location cache?
func (u User) Validate() error {
	if _, err := parseWeekday(u.ShoppingDay); err != nil {
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
		if _, err := strconv.Atoi(u.FavoriteStore); err != nil {
			return fmt.Errorf("invalid favorite store id %s: %w", u.FavoriteStore, err)
		}
	}
	// trim out recipes older than 2 months? store them in seperate file?
	slices.SortFunc(u.LastRecipes, func(a, b Recipe) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})

	return nil
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

func parseWeekday(v string) (time.Weekday, error) {
	for i := range daysOfWeek {
		if strings.EqualFold(daysOfWeek[i], v) {
			return time.Weekday(i), nil
		}
	}

	return time.Sunday, fmt.Errorf("invalid weekday '%s'", v)
}
