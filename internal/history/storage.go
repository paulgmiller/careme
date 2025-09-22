package history

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"
)

type Recipe struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	Ingredients  []string  `json:"ingredients"`
	Instructions []string  `json:"instructions"`
	CreatedAt    time.Time `json:"created_at"`
	Location     string    `json:"location"`
	Season       string    `json:"season"`
}

type HistoryStorage struct {
	storagePath   string
	retentionDays int
}

type History struct {
	Recipes []Recipe `json:"recipes"`
}

func NewHistoryStorage(storagePath string, retentionDays int) *HistoryStorage {
	return &HistoryStorage{
		storagePath:   storagePath,
		retentionDays: retentionDays,
	}
}

func (hs *HistoryStorage) SaveRecipes(recipes []Recipe) error {
	history, err := hs.loadHistory()
	if err != nil {
		return fmt.Errorf("failed to load existing history: %w", err)
	}

	for _, recipe := range recipes {
		recipe.CreatedAt = time.Now()
		history.Recipes = append(history.Recipes, recipe)
	}

	hs.cleanOldRecipes(&history)

	return hs.saveHistory(history)
}

func (hs *HistoryStorage) GetRecentRecipes(days int) ([]Recipe, error) {
	history, err := hs.loadHistory()
	if err != nil {
		return nil, fmt.Errorf("failed to load history: %w", err)
	}

	cutoff := time.Now().AddDate(0, 0, -days)
	var recentRecipes []Recipe

	for _, recipe := range history.Recipes {
		if recipe.CreatedAt.After(cutoff) {
			recentRecipes = append(recentRecipes, recipe)
		}
	}

	return recentRecipes, nil
}

func (hs *HistoryStorage) GetRecipeNames(days int) ([]string, error) {
	recipes, err := hs.GetRecentRecipes(days)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, recipe := range recipes {
		names = append(names, recipe.Name)
	}

	return names, nil
}

func (hs *HistoryStorage) HasRecipe(recipeName string, days int) (bool, error) {
	names, err := hs.GetRecipeNames(days)
	if err != nil {
		return false, err
	}

	for _, name := range names {
		if name == recipeName {
			return true, nil
		}
	}

	return false, nil
}

func (hs *HistoryStorage) loadHistory() (History, error) {
	var history History

	if err := hs.ensureStorageDir(); err != nil {
		return history, err
	}

	if _, err := os.Stat(hs.storagePath); os.IsNotExist(err) {
		return history, nil
	}

	data, err := ioutil.ReadFile(hs.storagePath)
	if err != nil {
		return history, fmt.Errorf("failed to read history file: %w", err)
	}

	if err := json.Unmarshal(data, &history); err != nil {
		return history, fmt.Errorf("failed to unmarshal history: %w", err)
	}

	return history, nil
}

func (hs *HistoryStorage) saveHistory(history History) error {
	if err := hs.ensureStorageDir(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal history: %w", err)
	}

	if err := ioutil.WriteFile(hs.storagePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write history file: %w", err)
	}

	return nil
}

func (hs *HistoryStorage) ensureStorageDir() error {
	dir := filepath.Dir(hs.storagePath)
	return os.MkdirAll(dir, 0755)
}

func (hs *HistoryStorage) cleanOldRecipes(history *History) {
	if hs.retentionDays <= 0 {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -hs.retentionDays)
	var keepRecipes []Recipe

	for _, recipe := range history.Recipes {
		if recipe.CreatedAt.After(cutoff) {
			keepRecipes = append(keepRecipes, recipe)
		}
	}

	history.Recipes = keepRecipes
}
