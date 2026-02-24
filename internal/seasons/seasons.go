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

// Style represents styling configuration including seasonal colors
type Style struct {
	Colors ColorScheme
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
			C50:  "oklch(98.0% 0.016 73.7)",
			C100: "oklch(95.4% 0.038 75.2)",
			C200: "oklch(90.1% 0.076 70.7)",
			C300: "oklch(83.7% 0.128 66.3)",
			C400: "oklch(75.0% 0.183 55.9)",
			C500: "oklch(70.5% 0.213 47.6)",
			C600: "oklch(64.6% 0.222 41.1)",
			C700: "oklch(55.3% 0.195 38.4)",
			C800: "oklch(47.0% 0.157 37.3)",
			C900: "oklch(40.8% 0.123 38.2)",
		}
	case Winter:
		// Blue/white colors - snow/ice white
		return ColorScheme{
			C50:  "oklch(97.7% 0.013 236.6)",
			C100: "oklch(95.1% 0.026 236.8)",
			C200: "oklch(90.1% 0.058 230.9)",
			C300: "oklch(82.8% 0.111 230.3)",
			C400: "oklch(74.6% 0.160 232.7)",
			C500: "oklch(68.5% 0.169 237.3)",
			C600: "oklch(58.8% 0.158 242.0)",
			C700: "oklch(50.0% 0.134 242.7)",
			C800: "oklch(44.3% 0.110 240.8)",
			C900: "oklch(39.1% 0.090 240.9)",
		}
	case Spring:
		// Green colors - growing plant green
		return ColorScheme{
			C50:  "oklch(98.2% 0.018 155.8)",
			C100: "oklch(96.2% 0.044 156.7)",
			C200: "oklch(92.5% 0.084 156.0)",
			C300: "oklch(87.1% 0.150 154.4)",
			C400: "oklch(79.2% 0.209 151.7)",
			C500: "oklch(72.3% 0.219 149.6)",
			C600: "oklch(62.7% 0.194 149.2)",
			C700: "oklch(52.7% 0.154 150.1)",
			C800: "oklch(44.8% 0.119 151.3)",
			C900: "oklch(39.3% 0.095 152.5)",
		}
	case Summer:
		// Yellow/golden colors - sunshine and ripe fruits
		return ColorScheme{
			C50:  "oklch(98.7% 0.026 102.2)",
			C100: "oklch(97.3% 0.071 103.2)",
			C200: "oklch(94.5% 0.129 101.5)",
			C300: "oklch(90.5% 0.182 98.1)",
			C400: "oklch(85.2% 0.199 91.9)",
			C500: "oklch(79.5% 0.184 86.0)",
			C600: "oklch(68.1% 0.162 75.8)",
			C700: "oklch(55.4% 0.135 66.4)",
			C800: "oklch(47.6% 0.114 61.9)",
			C900: "oklch(42.1% 0.095 57.7)",
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

// GetCurrentStyle returns the current style configuration
func GetCurrentStyle() Style {
	return Style{
		Colors: GetCurrentColorScheme(),
	}
}
