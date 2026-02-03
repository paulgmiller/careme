package users

import (
	"careme/internal/cache"
	"strings"
	"testing"
	"time"
)

func TestUserValidate(t *testing.T) {
	t.Run("valid user sorts recipes", func(t *testing.T) {
		newer := time.Date(2024, time.December, 1, 0, 0, 0, 0, time.UTC)
		older := newer.Add(-24 * time.Hour)
		oldest := newer.Add(-48 * time.Hour)
		user := &User{
			ID:            "user-1",
			ShoppingDay:   time.Monday.String(),
			Email:         []string{"alice@example.com"},
			FavoriteStore: "1234",
			LastRecipes: []Recipe{
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
		user := &User{
			ShoppingDay: "Caturday",
			Email:       []string{"bob@example.com"},
		}

		err := user.Validate()
		if err == nil || !strings.Contains(err.Error(), "invalid weekday") {
			t.Fatalf("expected invalid weekday error, got %v", err)
		}
	})

	t.Run("missing email", func(t *testing.T) {
		user := &User{ShoppingDay: time.Friday.String()}

		err := user.Validate()
		if err == nil || err.Error() != "at least one email is required" {
			t.Fatalf("expected missing email error, got %v", err)
		}
	})

	t.Run("invalid email address", func(t *testing.T) {
		user := &User{
			ShoppingDay: time.Saturday.String(),
			Email:       []string{"not-an-email"},
		}

		err := user.Validate()
		if err == nil || err.Error() != "invalid email address: not-an-email" {
			t.Fatalf("expected invalid email error, got %v", err)
		}
	})

	t.Run("invalid favorite store", func(t *testing.T) {
		user := &User{
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

func TestFindOrCreateByID(t *testing.T) {
	store := NewStorage(cache.NewFileCache(t.TempDir()))

	user, err := store.FindOrCreateByID("clerk_123", "Test@Example.com")
	if err != nil {
		t.Fatalf("expected user to be created, got error: %v", err)
	}
	if got, want := user.ID, "clerk_123"; got != want {
		t.Fatalf("unexpected user ID: got %s want %s", got, want)
	}
	if len(user.Email) != 1 || user.Email[0] != "test@example.com" {
		t.Fatalf("unexpected email list: %#v", user.Email)
	}

	byEmail, err := store.GetByEmail("test@example.com")
	if err != nil {
		t.Fatalf("expected user to be indexed by email, got error: %v", err)
	}
	if byEmail.ID != user.ID {
		t.Fatalf("email lookup returned wrong user: got %s want %s", byEmail.ID, user.ID)
	}
}

func TestFindOrCreateByIDAddsEmail(t *testing.T) {
	store := NewStorage(cache.NewFileCache(t.TempDir()))

	_, err := store.FindOrCreateByID("clerk_456", "first@example.com")
	if err != nil {
		t.Fatalf("expected user to be created, got error: %v", err)
	}

	user, err := store.FindOrCreateByID("clerk_456", "second@example.com")
	if err != nil {
		t.Fatalf("expected user to be updated, got error: %v", err)
	}
	if len(user.Email) != 2 {
		t.Fatalf("expected two emails, got %d", len(user.Email))
	}

	byEmail, err := store.GetByEmail("second@example.com")
	if err != nil {
		t.Fatalf("expected second email to be indexed, got error: %v", err)
	}
	if byEmail.ID != user.ID {
		t.Fatalf("email lookup returned wrong user: got %s want %s", byEmail.ID, user.ID)
	}
}
