package main

var fruit = []string{
	"bananas",
	"apples",
	"pears",
	"oranges",
	"cherries",
	"grapes",
	"strawberries",
	"blueberries",
	"raspberries",
	"blackberries",
	"watermelon",
	"cantaloupe",
	"honeydew melon",
	"kiwi",
	"pineapple",
	"mangoes",
}

var tubers = []string{
	"onions",
	"potatoes",
}

var vegetables = []string{
	// Leafy greens & lettuces
	"Romaine lettuce",
	"Green leaf lettuce",
	"Red leaf lettuce",
	"Iceberg lettuce",
	"Butterhead lettuce",
	"Little gem lettuce",
	"Spring mix",
	"Baby spring mix",
	"Arugula",
	"Baby arugula",
	"Spinach",
	"Baby spinach",
	"Kale",
	"Curly kale",
	"Lacinato kale",
	"Rainbow chard",
	"Bok choy",
	"Baby bok choy",
	"Napa cabbage",
	"Green cabbage",
	"Red cabbage",
	"Radicchio",

	// Brassicas
	"Broccoli",
	"Broccolini",
	"Cauliflower",
	"Brussels sprouts",

	// Roots & tubers
	"Carrots",
	"Baby carrots",
	"Rainbow carrots",
	"Beets",
	"Golden beets",
	"Turnips",
	"Rutabaga",
	"Parsnips",
	"Daikon radish",
	"Radishes",
	"Horseradish root",
	"Celery root (celeriac)",
	"Jicama",
	"Yuca (cassava)",

	// Alliums
	"Green onions (scallions)",
	"Leeks",
	"Garlic",

	// Stalks & stems
	"Celery",
	"Asparagus",
	"Lemongrass",

	// Fruiting vegetables
	"Green bell peppers",
	"Red bell peppers",
	"Yellow bell peppers",
	"Orange bell peppers",
	"Mini sweet peppers",
	"Poblano peppers",
	"Jalapeño peppers",
	"Serrano peppers",
	"Anaheim peppers",
	"Habanero peppers",
	"Red chili peppers",
	"Green chili peppers",
	"Tomatillos",
	"Zucchini",
	"Yellow squash",
	"Cucumber",
	"Mini cucumbers",
	"Seedless cucumbers",
	"Eggplant",
	"Green beans",
	"Sweet corn",

	// Mushrooms
	"White mushrooms",
	"Baby bella (cremini) mushrooms",
	"Portobello mushrooms",
	"Shiitake mushrooms",
	"Oyster mushrooms",
	"King trumpet mushrooms",
	"Sliced mushroom blend",

	// Herbs
	"Parsley",
	"Italian parsley",
	"Cilantro",
	"Basil",
	"Thyme",
	"Sage",
	"Rosemary",
	"Tarragon",
	"Dill",
	"Chives",

	// Sprouts & microgreens
	"Alfalfa sprouts",
	"Broccoli sprouts",
	"Mixed sprouts",

	// Other
	"Aloe vera leaf",
	"Bean sprouts",
}

var all = append(append(fruit, tubers...), vegetables...)
