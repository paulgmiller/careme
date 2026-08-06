package culinary

func Salt() Module {
	return Module{
		ID: "salt",
		RecipeRules: []string{
			"Presalting meat and salting pasta or blanching water season food during cooking. Do not reduce or omit those applications merely because salt or salty ingredients are added later; adjust finishing salt instead. Account for meat that is already brined or cured and for user requests to reduce sodium.",
		},
		CritiqueRules: []string{
			"when quantities permit calculation, use these salt amounts as starting points: 1.25% salt by weight for boneless meat, 1.5% for bone-in meat including roast chicken, 1% for vegetables and grains, and 2% salinity for pasta or vegetable-blanching water",
			"do not treat salt added later as a substitute for presalting meat or salting pasta or blanching water; salty ingredients added later may justify reducing finishing salt, but they do not correct food that was underseasoned during cooking",
			"account for ingredients that are already brined or cured and user requests to reduce sodium; because salt crystal sizes vary, evaluate salt by weight when available rather than assuming equal volume measures across salt types",
			"report a material deviation from these salt starting points as a flavor issue and suggest a corrected amount at the proper cooking stage; if it leaves a main component substantially underseasoned or oversalted, keep the overall score below 8 so the recipe is revised",
		},
	}
}
