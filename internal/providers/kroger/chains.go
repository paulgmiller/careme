package kroger

import "strings"

// Grocery banners use different storefront domains from their OAuth banner names.
// Keys come from the grocery banner values documented in openapi-auth.json;
// domains were manually paired with those banners, not fetched from the chains API.
// Keep this table shared by authorization, cart redirects, and review-link validation.
var chainDomains = map[string]string{
	"bakers":       "bakersplus.com",
	"citymarket":   "citymarket.com",
	"dillons":      "dillons.com",
	"food4less":    "food4less.com",
	"foodsco":      "foodsco.net",
	"fredmeyer":    "fredmeyer.com",
	"frys":         "frysfood.com",
	"gerbes":       "gerbes.com",
	"harristeeter": "harristeeter.com",
	"jaycfoods":    "jaycfoods.com",
	"kingsoopers":  "kingsoopers.com",
	"kroger":       "kroger.com",
	"marianos":     "marianos.com",
	"metromarket":  "metromarket.net",
	"payless":      "pay-less.com",
	"picknsave":    "picknsave.com",
	"qfc":          "qfc.com",
	"ralphs":       "ralphs.com",
	"rulerfoods":   "rulerfoods.com",
	"smiths":       "smithsfoodanddrug.com",
}

// authorizationBanner normalizes every chain name to an OAuth banner value.
// Most names match openapi-auth.json after lowercasing and removing punctuation
// and spaces (for example, "QFC" -> "qfc" and "Pick 'n Save" -> "picknsave").
// The switch handles names whose spelling also differs: FRED was observed in
// location data; the other aliases accept shortened or expanded store names.
// This alias list is manually maintained, not an exhaustive list from the API.
func authorizationBanner(chain string) string {
	banner := strings.NewReplacer(" ", "", "'", "", "’", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(chain)))
	switch banner {
	case "fred":
		return "fredmeyer"
	case "frysfood":
		return "frys"
	case "smithsfoodanddrug":
		return "smiths"
	case "jayc":
		return "jaycfoods"
	default:
		return banner
	}
}

func cartURL(chain string) string {
	domain, ok := chainDomains[authorizationBanner(chain)]
	if !ok {
		domain = chainDomains["kroger"]
	}
	return "https://www." + domain + "/cart"
}
