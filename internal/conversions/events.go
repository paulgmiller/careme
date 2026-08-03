package conversions

import "slices"

// Event identifies a neutral browser conversion published through Google Tag Manager.
type Event string

const (
	SignupCompleted  Event = "signup_completed"
	RecipeGeneration Event = "recipe_generation"
	RecipeSave       Event = "recipe_save"
)

var browserEvents = [...]Event{
	SignupCompleted,
	RecipeGeneration,
	RecipeSave,
}

// IsBrowserEvent reports whether event is safe to preserve and publish in the browser.
func IsBrowserEvent(event Event) bool {
	return slices.Contains(browserEvents[:], event)
}
