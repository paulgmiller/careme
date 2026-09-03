package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWeekday(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Weekday
		wantErr bool
	}{
		{
			name:  "sunday",
			input: "Sunday",
			want:  time.Sunday,
		},
		{
			name:  "case insensitive",
			input: "mOnDaY",
			want:  time.Monday,
		},
		{
			name:  "lowercase",
			input: "tuesday",
			want:  time.Tuesday,
		},
		{
			name:    "invalid",
			input:   "Caturday",
			want:    time.Sunday,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseWeekday(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

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

		require.NoError(t, user.Validate())

		assert.Equal(t, "newer", user.LastRecipes[0].Title)
		assert.Equal(t, "older", user.LastRecipes[1].Title)
		assert.Equal(t, "oldest", user.LastRecipes[2].Title)
	})

	t.Run("valid prefixed favorite store", func(t *testing.T) {
		user := &User{
			ID:            "user-1",
			ShoppingDay:   time.Monday.String(),
			Email:         []string{"alice@example.com"},
			FavoriteStore: "wholefoods_123",
		}

		require.NoError(t, user.Validate())
	})

	t.Run("invalid shopping day", func(t *testing.T) {
		user := &User{
			ShoppingDay: "Caturday",
			Email:       []string{"bob@example.com"},
		}

		err := user.Validate()
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid weekday")
	})

	t.Run("missing email", func(t *testing.T) {
		user := &User{ShoppingDay: time.Friday.String()}

		err := user.Validate()
		require.EqualError(t, err, "at least one email is required")
	})

	t.Run("invalid email address", func(t *testing.T) {
		user := &User{
			ShoppingDay: time.Saturday.String(),
			Email:       []string{"not-an-email"},
		}

		err := user.Validate()
		require.EqualError(t, err, "invalid email address: not-an-email")
	})

	t.Run("invalid favorite store", func(t *testing.T) {
		user := &User{
			ShoppingDay:   time.Sunday.String(),
			Email:         []string{"charlie@example.com"},
			FavoriteStore: "store-99",
		}

		err := user.Validate()
		require.Error(t, err)
		assert.ErrorContains(t, err, "invalid favorite store id")
	})

	t.Run("cannot be own pending partner", func(t *testing.T) {
		user := &User{
			ID:               "user-1",
			PendingPartnerID: "user-1",
			ShoppingDay:      time.Saturday.String(),
			Email:            []string{"alice@example.com"},
		}

		err := user.Validate()
		require.Error(t, err)
		assert.ErrorContains(t, err, "own pending partner")
	})

	t.Run("cannot have established and pending partners", func(t *testing.T) {
		user := &User{
			ID:               "user-1",
			PartnerID:        "user-2",
			PendingPartnerID: "user-3",
			ShoppingDay:      time.Saturday.String(),
			Email:            []string{"alice@example.com"},
		}

		err := user.Validate()
		require.Error(t, err)
		assert.ErrorContains(t, err, "pending partner")
	})
}
