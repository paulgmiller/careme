package kroger

import "strings"

// Grocery banners use different storefront domains from their OAuth banner names.
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

func validCartURL(destination string) bool {
	for _, domain := range chainDomains {
		if destination == "https://www."+domain+"/cart" {
			return true
		}
	}
	return false
}
