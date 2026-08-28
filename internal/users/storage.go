package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"

	utypes "careme/internal/users/types"

	"github.com/samber/lo"
)

type Storage struct {
	cache cache.ListCache
}

var ErrNotFound = errors.New("user not found")

const (
	CookieName  = "careme_user"
	userPrefix  = "users/"
	emailPrefix = "email2user/"

	shoppingListLimit  = 2
	shoppingListWindow = 7 * 24 * time.Hour
)

func NewStorage(c cache.ListCache) *Storage {
	return &Storage{cache: c}
}

// obviously needs to be better
func (s *Storage) List(ctx context.Context) ([]utypes.User, error) {
	userids, err := s.cache.List(ctx, userPrefix, "")
	if err != nil {
		return nil, err
	}
	var users []utypes.User
	for _, id := range userids {
		user, err := s.GetByID(id)
		if err != nil {
			return nil, err
		}
		users = append(users, *user)
	}
	return users, nil
}

func (s *Storage) GetByID(id string) (*utypes.User, error) {
	userBytes, err := s.cache.Get(context.TODO(), userPrefix+id)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer func() {
		if err := userBytes.Close(); err != nil {
			slog.Error("failed to close user reader", "error", err)
		}
	}()
	decoder := json.NewDecoder(userBytes)

	var user utypes.User
	if err := decoder.Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to unmarshal user: %w", err)
	}
	return &user, nil
}

func (s *Storage) GetByEmail(email string) (*utypes.User, error) {
	normalized := normalizeEmail(email)
	id, err := s.cache.Get(context.TODO(), emailPrefix+normalized)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer func() {
		if err := id.Close(); err != nil {
			slog.Error("failed to close user email reader", "error", err, "email", normalized)
		}
	}()
	data, err := io.ReadAll(id)
	if err != nil {
		return nil, fmt.Errorf("failed to read user ID: %w", err)
	}
	return s.GetByID(string(data))
}

type emailFetcher interface {
	GetUserEmail(ctx context.Context, userID string) (string, error)
}

func (s *Storage) FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error) {
	clerkUserID, err := authClient.GetUserIDFromRequest(r)
	if err != nil {
		return nil, err
	}
	return s.findOrCreateFromClerk(ctx, clerkUserID, authClient)
}

// interface for clerk client
func (s *Storage) findOrCreateFromClerk(ctx context.Context, clerkUserID string, emailFetcher emailFetcher) (*utypes.User, error) {
	user, err := s.GetByID(clerkUserID)
	if err == nil {
		return user, nil
	}

	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	primaryEmail, err := emailFetcher.GetUserEmail(ctx, clerkUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user email from clerk: %w", err)
	}

	newUser := utypes.User{
		ID:          clerkUserID, // do we need this o be independent for housholds?
		Email:       []string{normalizeEmail(primaryEmail)},
		CreatedAt:   time.Now(),
		ShoppingDay: time.Saturday.String(),
	}
	if err := s.Update(&newUser); err != nil {
		return nil, fmt.Errorf("failed to create new user: %w", err)
	}
	if err := s.cache.Put(context.TODO(), emailPrefix+newUser.Email[0], newUser.ID, cache.Unconditional()); err != nil {
		return nil, fmt.Errorf("failed to index new user by email: %w", err)
	}
	slog.InfoContext(ctx, "created new user", "id", clerkUserID, "email", primaryEmail)
	return &newUser, nil
}

func (s *Storage) Update(user *utypes.User) error {
	if err := user.Validate(); err != nil {
		return fmt.Errorf("invalid user: %w", err)
	}

	userBytes, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal user: %w", err)
	}
	if err := s.cache.Put(context.TODO(), userPrefix+user.ID, string(userBytes), cache.Unconditional()); err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

func (s *Storage) RemoveRecipe(user *utypes.User, recipeHash string) (bool, error) {
	recipeHash = strings.TrimSpace(recipeHash)
	if recipeHash == "" {
		return false, fmt.Errorf("invalid recipe hash")
	}
	if user == nil {
		return false, fmt.Errorf("user is required")
	}

	filtered := lo.Filter(user.LastRecipes, func(recipe utypes.Recipe, _ int) bool {
		return recipe.Hash != recipeHash
	})
	if len(filtered) == len(user.LastRecipes) {
		return false, nil // not found
	}

	user.LastRecipes = filtered
	if err := s.Update(user); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Storage) ReplaceRecipe(user *utypes.User, oldHash string, replacement utypes.Recipe) (bool, error) {
	oldHash = strings.TrimSpace(oldHash)
	replacement.Hash = strings.TrimSpace(replacement.Hash)
	if oldHash == "" || replacement.Hash == "" {
		return false, fmt.Errorf("invalid recipe hash")
	}
	if user == nil {
		return false, fmt.Errorf("user is required")
	}

	replaced := false
	for i := range user.LastRecipes {
		if user.LastRecipes[i].Hash == oldHash {
			user.LastRecipes[i] = replacement
			replaced = true
			break
		}
	}
	if !replaced {
		return false, nil
	}
	if err := s.Update(user); err != nil {
		return false, err
	}
	return true, nil
}

// RecordShoppingList keeps the newest completed shopping list for up to two
// store locations. Reloading the user avoids overwriting profile changes made
// while recipe generation was running in the background.
func (s *Storage) RecordShoppingList(userID string, shoppingList utypes.ShoppingList) error {
	userID = strings.TrimSpace(userID)
	shoppingList.Hash = strings.TrimSpace(shoppingList.Hash)
	shoppingList.LocationID = strings.TrimSpace(shoppingList.LocationID)
	shoppingList.LocationName = strings.TrimSpace(shoppingList.LocationName)
	shoppingList.LocationAddress = strings.TrimSpace(shoppingList.LocationAddress)
	if userID == "" || shoppingList.Hash == "" || shoppingList.LocationID == "" || shoppingList.CompletedAt.IsZero() {
		return fmt.Errorf("invalid shopping list")
	}

	user, err := s.GetByID(userID)
	if err != nil {
		return err
	}

	cutoff := shoppingList.CompletedAt.Add(-shoppingListWindow)
	recent := lo.Filter(user.ShoppingLists, func(existing utypes.ShoppingList, _ int) bool {
		return existing.LocationID != shoppingList.LocationID && !existing.CompletedAt.Before(cutoff)
	})
	user.ShoppingLists = append([]utypes.ShoppingList{shoppingList}, recent...)
	slices.SortFunc(user.ShoppingLists, func(a, b utypes.ShoppingList) int {
		return b.CompletedAt.Compare(a.CompletedAt)
	})
	user.ShoppingLists = lo.Take(user.ShoppingLists, shoppingListLimit)
	return s.Update(user)
}

// PruneShoppingLists removes stored shopping-list links that are more than
// seven days old. It returns true when the user record changed.
func (s *Storage) PruneShoppingLists(user *utypes.User, now time.Time) (bool, error) {
	if user == nil {
		return false, fmt.Errorf("user is required")
	}
	cutoff := now.Add(-shoppingListWindow)
	recent := lo.Filter(user.ShoppingLists, func(shoppingList utypes.ShoppingList, _ int) bool {
		return !shoppingList.CompletedAt.Before(cutoff)
	})
	slices.SortFunc(recent, func(a, b utypes.ShoppingList) int {
		return b.CompletedAt.Compare(a.CompletedAt)
	})
	recent = lo.Take(recent, shoppingListLimit)
	if slices.Equal(recent, user.ShoppingLists) {
		return false, nil
	}
	user.ShoppingLists = recent
	if err := s.Update(user); err != nil {
		return false, err
	}
	return true, nil
}

func normalizeEmail(email string) string {
	// remove . from before @? or +<suffix?
	return strings.TrimSpace(strings.ToLower(email))
}
