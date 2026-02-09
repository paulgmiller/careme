package users

import (
	utypes "careme/internal/users/types"
	"strings"
	"testing"
	"time"
)

func TestUserValidate(t *testing.T) {
	t.Run("valid user sorts recipes", func(t *testing.T) {
		newer := time.Date(2024, time.December, 1, 0, 0, 0, 0, time.UTC)
		older := newer.Add(-24 * time.Hour)
		oldest := newer.Add(-48 * time.Hour)
		user := &utypes.User{
			ID:            "user-1",
			ShoppingDay:   time.Monday.String(),
			Email:         []string{"alice@example.com"},
			FavoriteStore: "1234",
			LastRecipes: []utypes.Recipe{
				{Title: "newer", CreatedAt: newer},
				{Title: "oldest", CreatedAt: oldest},
				{Title: "older", CreatedAt: older},
			},
		}

		if err := user.Validate(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		for i, name := range []string{"newer", "older", "oldest"} {
			if got, want := user.LastRecipes[i].Title, name; got != want {
				t.Fatalf("recipes not sorted by CreatedAt: got %s want %s", got, want)
			}
		}
	})

	t.Run("invalid shopping day", func(t *testing.T) {
		user := &utypes.User{
			ShoppingDay: "Caturday",
			Email:       []string{"bob@example.com"},
		}

		err := user.Validate()
		if err == nil || !strings.Contains(err.Error(), "invalid weekday") {
			t.Fatalf("expected invalid weekday error, got %v", err)
		}
	})

	t.Run("missing email", func(t *testing.T) {
		user := &utypes.User{ShoppingDay: time.Friday.String()}

		err := user.Validate()
		if err == nil || err.Error() != "at least one email is required" {
			t.Fatalf("expected missing email error, got %v", err)
		}
	})

	t.Run("invalid email address", func(t *testing.T) {
		user := &utypes.User{
			ShoppingDay: time.Saturday.String(),
			Email:       []string{"not-an-email"},
		}

		err := user.Validate()
		if err == nil || err.Error() != "invalid email address: not-an-email" {
			t.Fatalf("expected invalid email error, got %v", err)
		}
	})

	t.Run("invalid favorite store", func(t *testing.T) {
		user := &utypes.User{
			ShoppingDay:   time.Sunday.String(),
			Email:         []string{"charlie@example.com"},
			FavoriteStore: "store-99",
		}

		err := user.Validate()
		if err == nil || !strings.Contains(err.Error(), "invalid favorite store id") {
			t.Fatalf("expected invalid favorite store error, got %v", err)
		}
	})
}
