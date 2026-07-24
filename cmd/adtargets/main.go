package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"careme/internal/cache"
	"careme/internal/campaigns"
	"careme/internal/config"
	"careme/internal/googleads"
	"careme/internal/locations"
	locationtypes "careme/internal/locations/types"
)

const (
	defaultCustomerID = "581-284-8025"
	defaultCampaignID = "23939758740"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	var customerID string
	var campaignID string
	var storeIDsCSV string
	var loginCustomerID string
	var radiusMiles float64
	var apply bool
	var timeoutSeconds int
	var targetOnly bool
	baseURL := "https://careme.cooking"
	var adStatus string
	var outputMode string

	fs := flag.NewFlagSet("adtargets", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.StringVar(&customerID, "customer-id", defaultCustomerID, "Google Ads customer ID")
	fs.StringVar(&campaignID, "campaign-id", defaultCampaignID, "Google Ads campaign ID")
	fs.StringVar(&storeIDsCSV, "store-ids", "", "Comma-separated location IDs")
	fs.StringVar(&loginCustomerID, "login-customer-id", "", "Optional Google Ads manager customer ID")
	fs.StringVar(&adStatus, "ad-status", "PAUSED", "Google Ads ad status for created store ads")
	fs.StringVar(&outputMode, "output", "api", "Output mode: api or manual")
	fs.Float64Var(&radiusMiles, "radius-miles", 2, "Proximity target radius in miles")
	fs.BoolVar(&apply, "apply", false, "Apply changes to Google Ads")
	fs.BoolVar(&targetOnly, "target-only", false, "Only sync campaign-level proximity targeting without creating store ad groups or ads")
	fs.IntVar(&timeoutSeconds, "timeout", 60, "Operation timeout in seconds")
	if err := fs.Parse(args); err != nil {
		return err
	}

	customerID = normalizeCustomerID(customerID)
	campaignID = strings.TrimSpace(campaignID)
	if customerID == "" {
		return fmt.Errorf("missing required -customer-id")
	}
	if campaignID == "" {
		return fmt.Errorf("missing required -campaign-id")
	}
	if radiusMiles <= 0 {
		return fmt.Errorf("-radius-miles must be greater than 0")
	}
	adStatus = strings.ToUpper(strings.TrimSpace(adStatus))
	if adStatus == "" {
		adStatus = "PAUSED"
	}
	outputMode = strings.ToLower(strings.TrimSpace(outputMode))
	if outputMode == "" {
		outputMode = "api"
	}

	storeIDs := uniqueStoreIDs(parseStoreIDs(storeIDsCSV))
	if len(storeIDs) == 0 {
		storeIDs = advertisedRecipeStoreIDs()
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	if err := config.LoadEncryptedEnv("secrets/envtest"); err != nil {
		return fmt.Errorf("load encrypted env: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// depends on config load for encrypted secrets
	adsConfig := googleads.ConfigFromEnv(loginCustomerID)

	cacheStore, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}
	locationStorage, err := locations.New(cfg, cacheStore, locations.LoadCentroids())
	if err != nil {
		return err
	}
	targets, err := hydrateTargets(ctx, locationStorage, storeIDs, radiusMiles, baseURL)
	if err != nil {
		return err
	}

	switch outputMode {
	case "manual":
		return printManualSteps(stdout, customerID, campaignID, adStatus, targets)
	case "api":
	default:
		return fmt.Errorf("unsupported -output %q; use api or manual", outputMode)
	}

	adsClient, err := googleads.NewClient(adsConfig)
	if err != nil {
		return err
	}
	if targetOnly {
		return runTargetOnlySync(ctx, stdout, adsClient, customerID, campaignID, radiusMiles, apply, targets)
	}

	return runStoreAdGroupSync(ctx, stdout, adsClient, customerID, campaignID, radiusMiles, apply, adStatus, targets)
}

func runTargetOnlySync(ctx context.Context, stdout io.Writer, adsClient *googleads.Client, customerID, campaignID string, radiusMiles float64, apply bool, targets []googleads.Target) error {
	existing, err := adsClient.SearchCampaignProximities(ctx, customerID, campaignID)
	if err != nil {
		return err
	}

	create, skip := missingProximityTargets(targets, existing)
	plan := googleads.Plan{Create: create, Skip: skip}
	if err := printPlan(stdout, "campaign proximity targeting", customerID, campaignID, radiusMiles, apply, targets, plan); err != nil {
		return err
	}
	if !apply {
		if _, err := fmt.Fprintln(stdout, "Dry run only. Re-run with -apply to mutate Google Ads."); err != nil {
			return err
		}
		return nil
	}

	// For now this command only ensures configured targets exist. It does not purge old targets.
	if _, err := adsClient.CreateProximityCriteria(ctx, customerID, campaignID, plan.Create); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "Applied %d creates and skipped %d existing targets.\n", len(plan.Create), len(plan.Skip)); err != nil {
		return err
	}
	return nil
}

func runStoreAdGroupSync(ctx context.Context, stdout io.Writer, adsClient *googleads.Client, customerID, campaignID string, radiusMiles float64, apply bool, adStatus string, targets []googleads.Target) error {
	existing, err := adsClient.SearchAdGroups(ctx, customerID, campaignID)
	if err != nil {
		return err
	}
	create, skip := missingAdGroupTargets(targets, existing)
	plan := googleads.Plan{Create: create, Skip: skip}
	if err := printPlan(stdout, "store ad groups", customerID, campaignID, radiusMiles, apply, targets, plan); err != nil {
		return err
	}
	if err := printStoreURLs(stdout, plan.Create); err != nil {
		return err
	}
	if !apply {
		if _, err := fmt.Fprintln(stdout, "Dry run only. Re-run with -apply to create ad groups, proximity targets, and ads."); err != nil {
			return err
		}
		return nil
	}

	// For now this command only ensures configured ads exist. It does not purge old ads.
	adGroups, err := adsClient.CreateAdGroups(ctx, customerID, campaignID, plan.Create)
	if err != nil {
		return err
	}
	criteria, err := adsClient.CreateAdGroupProximityCriteria(ctx, customerID, plan.Create, adGroups)
	if err != nil {
		return err
	}
	keywords, err := adsClient.CreateAdGroupKeywordCriteria(ctx, customerID, adGroups, defaultKeywords())
	if err != nil {
		return err
	}
	ads := make([]googleads.StoreAd, 0, len(plan.Create))
	for i, target := range plan.Create {
		ads = append(ads, googleads.StoreAd{
			AdGroup:      adGroups[i],
			FinalURL:     target.FinalURL,
			Status:       adStatus,
			Headlines:    defaultHeadlines(target),
			Descriptions: defaultDescriptions(target),
		})
	}
	adGroupAds, err := adsClient.CreateResponsiveSearchAds(ctx, customerID, ads)
	if err != nil {
		return err
	}

	expectedKeywords := len(plan.Create) * len(defaultKeywords())
	if len(plan.Create) != len(criteria) || expectedKeywords != len(keywords) || len(plan.Create) != len(adGroupAds) {
		return fmt.Errorf("created resources mismatch: targets=%d proximityCriteria=%d keywordCriteria=%d ads=%d", len(plan.Create), len(criteria), len(keywords), len(adGroupAds))
	}
	if _, err := fmt.Fprintf(stdout, "Applied %d store ad groups and skipped %d existing ad groups.\n", len(plan.Create), len(plan.Skip)); err != nil {
		return err
	}
	return nil
}

func missingProximityTargets(targets []googleads.Target, existing []googleads.ProximityCriterion) ([]googleads.Target, []googleads.Target) {
	existingShapes := make(map[string]struct{}, len(existing))
	for _, criterion := range existing {
		existingShapes[proximityKey(criterion.LatMicro, criterion.LonMicro, criterion.RadiusMiles)] = struct{}{}
	}

	create := make([]googleads.Target, 0, len(targets))
	skip := make([]googleads.Target, 0, len(targets))
	for _, target := range targets {
		if _, ok := existingShapes[proximityKey(target.LatMicro, target.LonMicro, target.RadiusMiles)]; ok {
			skip = append(skip, target)
			continue
		}
		create = append(create, target)
	}
	return create, skip
}

func proximityKey(latMicro, lonMicro int64, radiusMiles float64) string {
	return fmt.Sprintf("%d|%d|%g", latMicro, lonMicro, radiusMiles)
}

func missingAdGroupTargets(targets []googleads.Target, existing []googleads.AdGroupSummary) ([]googleads.Target, []googleads.Target) {
	existingNames := make(map[string]struct{}, len(existing))
	for _, adGroup := range existing {
		existingNames[strings.TrimSpace(adGroup.Name)] = struct{}{}
	}

	create := make([]googleads.Target, 0, len(targets))
	skip := make([]googleads.Target, 0, len(targets))
	for _, target := range targets {
		if _, ok := existingNames[googleads.AdGroupName(target)]; ok {
			skip = append(skip, target)
			continue
		}
		create = append(create, target)
	}
	return create, skip
}

func defaultHeadlines(_ googleads.Target) []string {
	return []string{
		"Dinner ideas nearby",
		"Cook fresh tonight",
		"Fresh meal ideas",
	}
}

func defaultDescriptions(_ googleads.Target) []string {
	return []string{
		"Get recipe ideas built around groceries near you.",
		"Find simple meals and build a grocery list for this store.",
	}
}

func defaultKeywords() []string {
	return []string{
		`"healthy local recipes"`,
		`"fresh local recipes"`,
		`"fresh dinner ideas"`,
		`"seasonal recipes"`,
		`"seasonal produce recipes"`,
		`"easy weeknight meals"`,
		`"what to cook tonight"`,
		`"vegetable recipes"`,
		`"fresh ingredient recipes"`,
		`"local grocery recipes"`,
	}
}

func printManualSteps(w io.Writer, customerID, campaignID, adStatus string, targets []googleads.Target) error {
	if _, err := fmt.Fprintf(w, "Manual Google Ads setup\ncustomer=%s campaign=%s stores=%d\n\n", customerID, campaignID, len(targets)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Campaign level: keep the shared budget, bidding, campaign dates, and campaign-wide assets here."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "Ad group level: create one ad group per store, with that store's 2-mile proximity target and keyword list."); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Ad level: create one responsive search ad in each ad group with the listed final URL, headlines, and descriptions. Create ads as %s, then review and enable them in Google Ads.\n\n", adStatus); err != nil {
		return err
	}

	for _, target := range targets {
		if _, err := fmt.Fprintf(w, "Store %s\n", target.StoreID); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "  Ad group level"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "    Name: %s\n", storeAdGroupName(target)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "    Proximity: %.6f, %.6f, %.2f miles\n", float64(target.LatMicro)/1_000_000, float64(target.LonMicro)/1_000_000, target.RadiusMiles); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "    Keywords: %s\n", strings.Join(defaultKeywords(), " | ")); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, "  Ad level"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "    Final URL: %s\n", target.FinalURL); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "    Headlines: %s\n", strings.Join(defaultHeadlines(target), " | ")); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "    Descriptions: %s\n\n", strings.Join(defaultDescriptions(target), " | ")); err != nil {
			return err
		}
	}
	return nil
}

func storeAdGroupName(target googleads.Target) string {
	return googleads.AdGroupName(target)
}

type locationGetter interface {
	GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error)
}

func hydrateTargets(ctx context.Context, locations locationGetter, storeIDs []string, radiusMiles float64, baseURL string) ([]googleads.Target, error) {
	targets := make([]googleads.Target, 0, len(storeIDs))
	for _, storeID := range storeIDs {
		loc, err := locations.GetLocationByID(ctx, storeID)
		if err != nil {
			return nil, fmt.Errorf("fetch location %s: %w", storeID, err)
		}
		if loc.Lat == nil || loc.Lon == nil {
			return nil, fmt.Errorf("location %s does not include latitude/longitude", storeID)
		}
		targets = append(targets, googleads.Target{
			StoreID:     loc.ID,
			StoreName:   loc.Name,
			Address:     loc.Address,
			LatMicro:    googleads.MicroDegrees(*loc.Lat),
			LonMicro:    googleads.MicroDegrees(*loc.Lon),
			RadiusMiles: radiusMiles,
			FinalURL:    recipeURL(baseURL, loc.ID),
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].StoreID < targets[j].StoreID
	})
	return targets, nil
}

func advertisedRecipeStoreIDs() []string {
	ids := make([]string, 0, len(campaigns.AdvertisedRecipeLocations()))
	for _, c := range campaigns.AdvertisedRecipeLocations() {
		ids = append(ids, c.Location.ID)
	}
	return ids
}

func parseStoreIDs(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	ids := make([]string, 0, len(fields))
	for _, field := range fields {
		id := strings.TrimSpace(field)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func uniqueStoreIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	sort.Strings(unique)
	return unique
}

func normalizeCustomerID(id string) string {
	return strings.ReplaceAll(strings.TrimSpace(id), "-", "")
}

func recipeURL(baseURL, storeID string) string {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		u = &url.URL{Scheme: "https", Host: "careme.cooking"}
	}
	u.Path = "/recipes"
	q := u.Query()
	q.Set("location", storeID)
	u.RawQuery = q.Encode()
	return u.String()
}

func printPlan(w io.Writer, modeName, customerID, campaignID string, radiusMiles float64, apply bool, targets []googleads.Target, plan googleads.Plan) error {
	mode := "dry-run"
	if apply {
		mode = "apply"
	}
	if _, err := fmt.Fprintf(w, "Google Ads ad targets %s (%s)\n", modeName, mode); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "customer=%s campaign=%s radius=%.2f miles stores=%d\n", customerID, campaignID, radiusMiles, len(targets)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "create=%d skip_existing=%d\n\n", len(plan.Create), len(plan.Skip)); err != nil {
		return err
	}

	if err := printTargets(w, "Create", plan.Create); err != nil {
		return err
	}
	if err := printTargets(w, "Skip existing manual targets", plan.Skip); err != nil {
		return err
	}
	return nil
}

func printStoreURLs(w io.Writer, targets []googleads.Target) error {
	if len(targets) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "Store ad URLs:"); err != nil {
		return err
	}
	for _, target := range targets {
		if _, err := fmt.Fprintf(w, "  %s -> %s\n", target.StoreID, target.FinalURL); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func printTargets(w io.Writer, title string, targets []googleads.Target) error {
	if len(targets) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "%s:\n", title); err != nil {
		return err
	}
	for _, target := range targets {
		if _, err := fmt.Fprintf(w, "  %s %s (%s) lat_micro=%d lon_micro=%d radius=%.2f\n", target.StoreID, target.StoreName, target.Address, target.LatMicro, target.LonMicro, target.RadiusMiles); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}
