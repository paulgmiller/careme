package culinary

func Browning() Module {
	return Module{
		ID: "browning",
		RecipeRules: []string{
			"When browning or crisping food, remove excess surface moisture when appropriate, adequately heat the pan or oven before adding the food, and leave enough space for moisture to escape instead of steaming the food.",
			"When browning mushrooms, wait until they have browned before adding salt.",
		},
		CritiqueRules: []string{
			"when a recipe promises browned, seared, roasted, or crisp food, check that its instructions remove excess surface moisture when appropriate, adequately heat the cooking surface or oven, and avoid crowding, covering, or tight packing that would trap steam",
			"when browning mushrooms, check that salt is added after the mushrooms have browned rather than before",
			"report a material conflict between the instructions and the promised browned or crisp texture as a cookability issue; if the main advertised texture cannot be achieved, keep the overall score below 8 so the recipe is revised",
		},
	}
}
