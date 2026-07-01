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

func (s *Storage) LinkPartners(inviter *utypes.User, partnerEmail string) (*utypes.User, error) {
	if inviter == nil {
		return nil, fmt.Errorf("user is required")
	}
	partner, err := s.GetByEmail(partnerEmail)
	if err != nil {
		return nil, err
	}
	if partner.ID == inviter.ID {
		return nil, fmt.Errorf("choose someone else's email for your kitchen partner")
	}
	if inviter.PartnerUserID != "" && inviter.PartnerUserID != partner.ID {
		return nil, fmt.Errorf("this kitchen already has a partner")
	}
	if partner.PartnerUserID != "" && partner.PartnerUserID != inviter.ID {
		return nil, fmt.Errorf("that chef already has a kitchen partner")
	}

	inviter.PartnerUserID = partner.ID
	partner.PartnerUserID = inviter.ID
	partner.FavoriteStore = inviter.FavoriteStore
	partner.ShoppingDay = inviter.ShoppingDay
	partner.Directive = inviter.Directive
	partner.LastRecipes = mergeRecipes(inviter.LastRecipes, partner.LastRecipes)
	inviter.LastRecipes = partner.LastRecipes

	if err := s.updateWithoutPartnerSync(partner); err != nil {
		return nil, err
	}
	if err := s.Update(inviter); err != nil {
		return nil, err
	}
	return partner, nil
}

func (s *Storage) KitchenUser(user *utypes.User) (*utypes.User, error) {
	if user == nil || user.PartnerUserID == "" {
		return user, nil
	}
	partner, err := s.GetByID(user.PartnerUserID)
	if err != nil {
		return user, err
	}
	user.LastRecipes = mergeRecipes(user.LastRecipes, partner.LastRecipes)
	return user, nil
}

type emailFetcher interface {
	GetUserEmail(ctx context.Context, userID string) (string, error)
}

func (s *Storage) FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error) {
	clerkUserID, err := authClient.GetUserIDFromRequest(r)
	if err != nil {
		return nil, err
	}
	user, err := s.findOrCreateFromClerk(ctx, clerkUserID, authClient)
	if err != nil {
		return nil, err
	}
	return s.KitchenUser(user)
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
	if err := s.updateWithoutPartnerSync(user); err != nil {
		return err
	}
	return s.syncPartner(user)
}

func (s *Storage) updateWithoutPartnerSync(user *utypes.User) error {
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
	for _, email := range user.Email {
		if err := s.cache.Put(context.TODO(), emailPrefix+normalizeEmail(email), user.ID, cache.Unconditional()); err != nil {
			return fmt.Errorf("failed to index user by email: %w", err)
		}
	}
	return nil
}

func (s *Storage) syncPartner(user *utypes.User) error {
	if user == nil || strings.TrimSpace(user.PartnerUserID) == "" {
		return nil
	}
	partner, err := s.GetByID(user.PartnerUserID)
	if err != nil {
		return fmt.Errorf("failed to load kitchen partner: %w", err)
	}
	if partner.PartnerUserID != user.ID {
		return fmt.Errorf("kitchen partner link is not mutual")
	}
	partner.FavoriteStore = user.FavoriteStore
	partner.ShoppingDay = user.ShoppingDay
	partner.Directive = user.Directive
	partner.LastRecipes = mergeRecipes(user.LastRecipes, partner.LastRecipes)
	user.LastRecipes = partner.LastRecipes
	return s.updateWithoutPartnerSync(partner)
}

func mergeRecipes(a, b []utypes.Recipe) []utypes.Recipe {
	byHash := make(map[string]utypes.Recipe, len(a)+len(b))
	for _, recipe := range b {
		byHash[recipe.Hash] = recipe
	}
	for _, recipe := range a {
		byHash[recipe.Hash] = recipe
	}
	merged := make([]utypes.Recipe, 0, len(byHash))
	for _, recipe := range byHash {
		merged = append(merged, recipe)
	}
	slices.SortFunc(merged, func(a, b utypes.Recipe) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})
	return merged
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

func normalizeEmail(email string) string {
	// remove . from before @? or +<suffix?
	return strings.TrimSpace(strings.ToLower(email))
}
