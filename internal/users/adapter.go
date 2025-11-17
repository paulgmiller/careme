package users

// UserStoreAdapter adapts Storage to provide a simplified interface for updating favorite stores
type UserStoreAdapter struct {
	storage *Storage
}

// NewUserStoreAdapter creates a new adapter
func NewUserStoreAdapter(storage *Storage) *UserStoreAdapter {
	return &UserStoreAdapter{storage: storage}
}

// GetUserByID retrieves user information
func (a *UserStoreAdapter) GetUserByID(id string) (userID string, favoriteStore string, err error) {
	user, err := a.storage.GetByID(id)
	if err != nil {
		return "", "", err
	}
	return user.ID, user.FavoriteStore, nil
}

// UpdateUserFavoriteStore updates the user's favorite store
func (a *UserStoreAdapter) UpdateUserFavoriteStore(userID string, storeID string) error {
	user, err := a.storage.GetByID(userID)
	if err != nil {
		return err
	}
	user.FavoriteStore = storeID
	return a.storage.Update(user)
}
