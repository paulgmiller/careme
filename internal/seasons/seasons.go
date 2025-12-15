package seasons

import "time"

// Season represents a season of the year
type Season string

const (
	Fall   Season = "fall"
	Winter Season = "winter"
	Spring Season = "spring"
	Summer Season = "summer"
)

// ColorScheme represents a Tailwind color palette for a season
type ColorScheme struct {
	C50  string
	C100 string
	C200 string
	C300 string
	C400 string
	C500 string
	C600 string
	C700 string
	C800 string
	C900 string
}

// GetSeason determines the season based on the month
func GetSeason(t time.Time) Season {
	month := t.Month()

	// Fall: September, October, November
	if month >= time.September && month <= time.November {
		return Fall
	}

	// Winter: December, January, February
	if month == time.December || month <= time.February {
		return Winter
	}

	// Spring: March, April, May
	if month >= time.March && month <= time.May {
		return Spring
	}

	// Summer: June, July, August
	return Summer
}

// GetColorScheme returns the appropriate color scheme for the given season
func GetColorScheme(season Season) ColorScheme {
	switch season {
	case Fall:
		// Orange colors - leaf falling orange
		return ColorScheme{
			C50:  "#fff7ed",
			C100: "#ffedd5",
			C200: "#fed7aa",
			C300: "#fdba74",
			C400: "#fb923c",
			C500: "#f97316",
			C600: "#ea580c",
			C700: "#c2410c",
			C800: "#9a3412",
			C900: "#7c2d12",
		}
	case Winter:
		// Blue/white colors - snow/ice white
		return ColorScheme{
			C50:  "#f0f9ff",
			C100: "#e0f2fe",
			C200: "#bae6fd",
			C300: "#7dd3fc",
			C400: "#38bdf8",
			C500: "#0ea5e9",
			C600: "#0284c7",
			C700: "#0369a1",
			C800: "#075985",
			C900: "#0c4a6e",
		}
	case Spring:
		// Green colors - growing plant green
		return ColorScheme{
			C50:  "#f0fdf4",
			C100: "#dcfce7",
			C200: "#bbf7d0",
			C300: "#86efac",
			C400: "#4ade80",
			C500: "#22c55e",
			C600: "#16a34a",
			C700: "#15803d",
			C800: "#166534",
			C900: "#14532d",
		}
	case Summer:
		// Yellow/golden colors - sunshine and ripe fruits
		return ColorScheme{
			C50:  "#fefce8",
			C100: "#fef9c3",
			C200: "#fef08a",
			C300: "#fde047",
			C400: "#facc15",
			C500: "#eab308",
			C600: "#ca8a04",
			C700: "#a16207",
			C800: "#854d0e",
			C900: "#713f12",
		}
	default:
		// Default to fall colors
		return GetColorScheme(Fall)
	}
}

// GetCurrentSeason returns the current season based on the current time
func GetCurrentSeason() Season {
	return GetSeason(time.Now())
}

// GetCurrentColorScheme returns the color scheme for the current season
func GetCurrentColorScheme() ColorScheme {
	return GetColorScheme(GetCurrentSeason())
}
