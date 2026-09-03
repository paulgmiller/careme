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

var (
	ErrNotFound                = errors.New("user not found")
	ErrPartnerNotFound         = errors.New("partner account not found")
	ErrPartnerSelf             = errors.New("user cannot partner with themselves")
	ErrUserAlreadyHasPartner   = errors.New("user already has a partner")
	ErrPartnerUnavailable      = errors.New("partner is unavailable")
	ErrNoPartner               = errors.New("user does not have a partner")
	ErrNoIncomingPartner       = errors.New("user does not have an incoming partner request")
	ErrPartnershipInconsistent = errors.New("partnership is not reciprocal")
)

const (
	CookieName         = "careme_user"
	userPrefix         = "users/"
	emailPrefix        = "email2user/"
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
	if err := normalizeShoppingLists(&user, time.Now()); err != nil {
		return nil, fmt.Errorf("invalid user shopping lists: %w", err)
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
	return s.writeUser(user)
}

func (s *Storage) writeUser(user *utypes.User) error {
	if err := user.Validate(); err != nil {
		return fmt.Errorf("invalid user: %w", err)
	}
	if err := normalizeShoppingLists(user, time.Now()); err != nil {
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

func (s *Storage) partner(user *utypes.User) (*utypes.User, error) {
	if user == nil {
		return nil, fmt.Errorf("user is required")
	}

	partnerID := user.PartnerID
	if user.PendingPartnerID != "" {
		partnerID = user.PendingPartnerID
	}
	if partnerID == "" {
		return nil, nil
	}
	partner, err := s.GetByID(partnerID)
	if err != nil {
		return nil, fmt.Errorf("load partner: %w", errors.Join(ErrPartnershipInconsistent, err))
	}
	if partnershipStageFor(user, partner) == partnershipStageNone {
		return nil, ErrPartnershipInconsistent
	}
	return partner, nil
}

type partnershipStage string

const (
	partnershipStageNone     partnershipStage = "none"
	partnershipStageOutgoing partnershipStage = "outgoing"
	partnershipStageIncoming partnershipStage = "incoming"
	partnershipStageLinked   partnershipStage = "linked"
)

func partnershipStageFor(user, partner *utypes.User) partnershipStage {
	if user == nil || partner == nil {
		return partnershipStageNone
	}
	switch {
	case user.PartnerID == partner.ID && partner.PartnerID == user.ID:
		return partnershipStageLinked
	case user.PartnerID == partner.ID && partner.PendingPartnerID == user.ID:
		return partnershipStageOutgoing
	case user.PendingPartnerID == partner.ID && partner.PartnerID == user.ID:
		return partnershipStageIncoming
	default:
		return partnershipStageNone
	}
}

func (s *Storage) RequestPartner(currentUser *utypes.User, email string) error {

	partner, err := s.GetByEmail(email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrPartnerNotFound
		}
		return fmt.Errorf("find partner by email: %w", err)
	}
	if currentUser.ID == partner.ID {
		return ErrPartnerSelf
	}
	if currentUser.PartnerID != "" || currentUser.PendingPartnerID != "" {
		return ErrUserAlreadyHasPartner
	}
	if partner.PartnerID != "" || partner.PendingPartnerID != "" {
		return ErrPartnerUnavailable
	}

	currentUser.PartnerID = partner.ID
	// TODO: Partnership updates span two user records and are not atomic. Concurrent
	// requests handled by different servers can leave the relationship inconsistent.
	// Move this to transactional storage or use ETags for conditional writes.
	if err := s.writeUser(currentUser); err != nil {
		return fmt.Errorf("save outgoing partner request: %w", err)
	}
	partner.PendingPartnerID = currentUser.ID
	if err := s.writeUser(partner); err != nil {
		currentUser.PartnerID = ""
		rollbackErr := s.writeUser(currentUser)
		return fmt.Errorf("save incoming partner request: %w", errors.Join(err, rollbackErr))
	}
	return nil
}

func (s *Storage) AcceptPartner(currentUser *utypes.User) error {

	if currentUser.PendingPartnerID == "" || currentUser.PartnerID != "" {
		return ErrNoIncomingPartner
	}
	partner, err := s.GetByID(currentUser.PendingPartnerID)
	if err != nil {
		return fmt.Errorf("load partner: %w", errors.Join(ErrPartnershipInconsistent, err))
	}
	if partnershipStageFor(currentUser, partner) != partnershipStageIncoming {
		return ErrPartnershipInconsistent
	}

	currentUser.PartnerID = currentUser.PendingPartnerID
	currentUser.PendingPartnerID = ""
	if err := s.writeUser(currentUser); err != nil {
		return fmt.Errorf("accept partner: %w", err)
	}
	return nil
}

func (s *Storage) UnlinkPartner(currentUser *utypes.User) error {
	partnerID := currentUser.PartnerID
	if currentUser.PendingPartnerID != "" {
		partnerID = currentUser.PendingPartnerID
	}
	if partnerID == "" {
		return ErrNoPartner
	}
	partner, err := s.GetByID(partnerID)
	if err != nil {
		return fmt.Errorf("load partner: %w", errors.Join(ErrPartnershipInconsistent, err))
	}
	if partnershipStageFor(currentUser, partner) == partnershipStageNone {
		return ErrPartnershipInconsistent
	}

	currentPartnerID := currentUser.PartnerID
	currentPendingPartnerID := currentUser.PendingPartnerID
	currentUser.PartnerID = ""
	currentUser.PendingPartnerID = ""
	if err := s.writeUser(currentUser); err != nil {
		return fmt.Errorf("unlink current user: %w", err)
	}
	partner.PartnerID = ""
	partner.PendingPartnerID = ""
	if err := s.writeUser(partner); err != nil {
		currentUser.PartnerID = currentPartnerID
		currentUser.PendingPartnerID = currentPendingPartnerID
		rollbackErr := s.writeUser(currentUser)
		return fmt.Errorf("unlink partner: %w", errors.Join(err, rollbackErr))
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

func normalizeShoppingLists(user *utypes.User, now time.Time) error {
	if user == nil {
		return fmt.Errorf("user is required")
	}
	for i := range user.ShoppingLists {
		user.ShoppingLists[i].Hash = strings.TrimSpace(user.ShoppingLists[i].Hash)
		user.ShoppingLists[i].Name = strings.TrimSpace(user.ShoppingLists[i].Name)
		if user.ShoppingLists[i].Hash == "" || user.ShoppingLists[i].Name == "" || user.ShoppingLists[i].CompletedAt.IsZero() {
			return fmt.Errorf("shopping list at index %d is invalid", i)
		}
	}

	cutoff := now.Add(-shoppingListWindow)
	recent := lo.Filter(user.ShoppingLists, func(shoppingList utypes.ShoppingList, _ int) bool {
		return !shoppingList.CompletedAt.Before(cutoff)
	})
	slices.SortFunc(recent, func(a, b utypes.ShoppingList) int {
		return b.CompletedAt.Compare(a.CompletedAt)
	})

	unique := make([]utypes.ShoppingList, 0, min(len(recent), shoppingListLimit))
	for _, shoppingList := range recent {
		if _, found := lo.Find(unique, func(existing utypes.ShoppingList) bool {
			return strings.EqualFold(existing.Name, shoppingList.Name)
		}); found {
			continue
		}
		unique = append(unique, shoppingList)
		if len(unique) == shoppingListLimit {
			break
		}
	}
	user.ShoppingLists = unique
	return nil
}

func normalizeEmail(email string) string {
	// remove . from before @? or +<suffix?
	return strings.TrimSpace(strings.ToLower(email))
}
