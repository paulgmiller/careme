package ai

import "careme/internal/culinary"

var caremeCulinaryGuidance = culinary.DefaultLibrary()

var systemMessage = buildRecipeSystemMessage(caremeCulinaryGuidance)

var recipeCritiqueSystemInstruction = buildRecipeCritiqueSystemInstruction(caremeCulinaryGuidance)

func buildRecipeSystemMessage(guidance culinary.Library) string {
	return baseRecipeSystemMessage + guidance.RecipePrompt()
}

func buildRecipeCritiqueSystemInstruction(guidance culinary.Library) string {
	return baseRecipeCritiqueSystemInstruction + guidance.CritiquePrompt() + recipeCritiqueOutputInstruction
}
