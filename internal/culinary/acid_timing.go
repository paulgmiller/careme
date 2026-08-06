package culinary

func AcidTiming() Module {
	return Module{
		ID: "acid-timing",
		RecipeRules: []string{
			"When tenderness is desired, cook onions and other firm vegetables until appropriately tender before adding acidic ingredients such as tomatoes, wine, vinegar, or citrus.",
			"When appropriate, add a small finishing amount of acid after cooking to brighten a rich dish rather than relying only on acid cooked for a long time.",
		},
		CritiqueRules: []string{
			"when tenderness is intended, check that onions and other firm vegetables soften before acidic ingredients such as tomatoes, wine, vinegar, or citrus are added",
			"allow acid earlier when the recipe intentionally preserves firmness, pickles or marinates the ingredient, or provides enough cooking time to achieve the stated texture",
			"consider whether a rich dish needs a fresh finishing source of acid for balance, but do not require one when the dish is already balanced",
			"report acid timing that is likely to prevent the main ingredient from reaching its promised texture as a cookability issue; if the promised texture is unlikely to be achieved, keep the overall score below 8 so the recipe is revised",
		},
	}
}
