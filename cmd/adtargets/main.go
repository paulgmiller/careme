package main

import (
	"bufio"
	"context"
	"encoding/csv"
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
	var inputPath string
	var loginCustomerID string
	var radiusMiles float64
	var apply bool
	var timeoutSeconds int
	var targetOnly bool
	var baseURL string
	var adStatus string
	var outputMode string

	fs := flag.NewFlagSet("adtargets", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.StringVar(&customerID, "customer-id", defaultCustomerID, "Google Ads customer ID")
	fs.StringVar(&campaignID, "campaign-id", defaultCampaignID, "Google Ads campaign ID")
	fs.StringVar(&storeIDsCSV, "store-ids", "", "Comma-separated location IDs")
	fs.StringVar(&inputPath, "input", "", "Path to CSV/TXT file containing location IDs")
	fs.StringVar(&loginCustomerID, "login-customer-id", "", "Optional Google Ads manager customer ID")
	fs.StringVar(&baseURL, "base-url", "https://careme.cooking", "Public base URL for store recipe landing pages")
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

	storeIDs, err := readStoreIDs(storeIDsCSV, inputPath)
	if err != nil {
		return err
	}
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
	if loginCustomerID != "" {
		cfg.GoogleAds.LoginCustomerID = loginCustomerID
	}

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

	registry, err := googleads.LoadRegistry(ctx, cacheStore, customerID, campaignID)
	if err != nil {
		return err
	}

	adsClient, err := googleads.NewClient(googleads.ConfigFromApp(cfg.GoogleAds))
	if err != nil {
		return err
	}
	if targetOnly {
		return runTargetOnlySync(ctx, stdout, adsClient, cacheStore, registry, customerID, campaignID, radiusMiles, apply, targets)
	}

	return runStoreAdGroupSync(ctx, stdout, adsClient, cacheStore, registry, customerID, campaignID, radiusMiles, apply, adStatus, targets)
}

func runTargetOnlySync(ctx context.Context, stdout io.Writer, adsClient *googleads.Client, cacheStore cache.ListCache, registry googleads.Registry, customerID, campaignID string, radiusMiles float64, apply bool, targets []googleads.Target) error {
	existing, err := adsClient.SearchCampaignProximities(ctx, customerID, campaignID)
	if err != nil {
		return err
	}

	plan := googleads.PlanSync(targets, registry.Entries, existing)
	if err := printPlan(stdout, "campaign proximity targeting", customerID, campaignID, radiusMiles, apply, targets, plan); err != nil {
		return err
	}
	if !apply {
		if _, err := fmt.Fprintln(stdout, "Dry run only. Re-run with -apply to mutate Google Ads."); err != nil {
			return err
		}
		return nil
	}

	removeResourceNames := make([]string, 0, len(plan.Remove))
	for _, entry := range plan.Remove {
		removeResourceNames = append(removeResourceNames, entry.ResourceName)
	}
	if err := adsClient.RemoveCampaignCriteria(ctx, customerID, removeResourceNames); err != nil {
		return err
	}
	registry = googleads.RemoveEntries(registry, append(plan.Remove, plan.Forget...))
	registry.CustomerID = customerID
	registry.CampaignID = campaignID
	if err := googleads.SaveRegistry(ctx, cacheStore, registry); err != nil {
		return err
	}

	resourceNames, err := adsClient.CreateProximityCriteria(ctx, customerID, campaignID, plan.Create)
	if err != nil {
		return err
	}
	registry, err = googleads.ApplyCreatedEntries(registry, plan.Create, resourceNames)
	if err != nil {
		return err
	}
	if err := googleads.SaveRegistry(ctx, cacheStore, registry); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "Applied %d creates, %d removals, and %d registry forgets.\n", len(plan.Create), len(plan.Remove), len(plan.Forget)); err != nil {
		return err
	}
	return nil
}

func runStoreAdGroupSync(ctx context.Context, stdout io.Writer, adsClient *googleads.Client, cacheStore cache.ListCache, registry googleads.Registry, customerID, campaignID string, radiusMiles float64, apply bool, adStatus string, targets []googleads.Target) error {
	plan := googleads.PlanSync(targets, registry.Entries, nil)
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

	removeAdGroups := make([]string, 0, len(plan.Remove))
	for _, entry := range plan.Remove {
		if entry.AdGroup != "" {
			removeAdGroups = append(removeAdGroups, entry.AdGroup)
		}
	}
	if err := adsClient.RemoveAdGroups(ctx, customerID, removeAdGroups); err != nil {
		return err
	}
	registry = googleads.RemoveEntries(registry, append(plan.Remove, plan.Forget...))
	registry.CustomerID = customerID
	registry.CampaignID = campaignID
	if err := googleads.SaveRegistry(ctx, cacheStore, registry); err != nil {
		return err
	}

	adGroups, err := adsClient.CreateAdGroups(ctx, customerID, campaignID, plan.Create)
	if err != nil {
		return err
	}
	criteria, err := adsClient.CreateAdGroupProximityCriteria(ctx, customerID, plan.Create, adGroups)
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

	registry, err = applyCreatedStoreAdGroups(registry, plan.Create, adGroups, criteria, adGroupAds)
	if err != nil {
		return err
	}
	if err := googleads.SaveRegistry(ctx, cacheStore, registry); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "Applied %d store ad groups, %d removals, and %d registry forgets.\n", len(plan.Create), len(plan.Remove), len(plan.Forget)); err != nil {
		return err
	}
	return nil
}

func applyCreatedStoreAdGroups(registry googleads.Registry, targets []googleads.Target, adGroups, criteria, adGroupAds []string) (googleads.Registry, error) {
	if len(targets) != len(adGroups) || len(targets) != len(criteria) || len(targets) != len(adGroupAds) {
		return googleads.Registry{}, fmt.Errorf("created resources mismatch: targets=%d ad_groups=%d criteria=%d ads=%d", len(targets), len(adGroups), len(criteria), len(adGroupAds))
	}

	entriesByStore := make(map[string]googleads.RegistryEntry, len(registry.Entries)+len(targets))
	for _, entry := range registry.Entries {
		entriesByStore[entry.StoreID] = entry
	}
	for i, target := range targets {
		entriesByStore[target.StoreID] = googleads.RegistryEntry{
			StoreID:      target.StoreID,
			StoreName:    target.StoreName,
			Address:      target.Address,
			LatMicro:     target.LatMicro,
			LonMicro:     target.LonMicro,
			RadiusMiles:  target.RadiusMiles,
			ResourceName: criteria[i],
			FinalURL:     target.FinalURL,
			AdGroup:      adGroups[i],
			AdGroupAd:    adGroupAds[i],
		}
	}

	registry.Entries = registry.Entries[:0]
	for _, entry := range entriesByStore {
		registry.Entries = append(registry.Entries, entry)
	}
	sort.Slice(registry.Entries, func(i, j int) bool {
		return registry.Entries[i].StoreID < registry.Entries[j].StoreID
	})
	return registry, nil
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
	name := strings.TrimSpace(target.StoreName)
	if name == "" {
		name = "Store"
	}
	return fmt.Sprintf("Careme Store %s %s", target.StoreID, name)
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

func readStoreIDs(storeIDsCSV, inputPath string) ([]string, error) {
	ids := parseStoreIDs(storeIDsCSV)
	if inputPath != "" {
		fromFile, err := readStoreIDsFromFile(inputPath)
		if err != nil {
			return nil, err
		}
		ids = append(ids, fromFile...)
	}
	return uniqueStoreIDs(ids), nil
}

func advertisedRecipeStoreIDs() []string {
	ids := make([]string, 0, len(campaigns.AdvertisedRecipeLocations()))
	for _, location := range campaigns.AdvertisedRecipeLocations() {
		ids = append(ids, location.Location.ID)
	}
	return uniqueStoreIDs(ids)
}

func readStoreIDsFromFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err == nil {
		var ids []string
		for _, row := range rows {
			if len(row) == 0 {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(row[0]), "store_id") || strings.EqualFold(strings.TrimSpace(row[0]), "location_id") {
				continue
			}
			ids = append(ids, parseStoreIDs(row[0])...)
		}
		return ids, nil
	}

	if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	var ids []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		ids = append(ids, parseStoreIDs(scanner.Text())...)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return ids, nil
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
	if _, err := fmt.Fprintf(w, "create=%d remove=%d forget=%d keep=%d skip_existing=%d\n\n", len(plan.Create), len(plan.Remove), len(plan.Forget), len(plan.Keep), len(plan.Skip)); err != nil {
		return err
	}

	if err := printTargets(w, "Create", plan.Create); err != nil {
		return err
	}
	if err := printEntries(w, "Remove managed stale targets", plan.Remove); err != nil {
		return err
	}
	if err := printEntries(w, "Forget missing managed targets", plan.Forget); err != nil {
		return err
	}
	if err := printEntries(w, "Keep managed targets", plan.Keep); err != nil {
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

func printEntries(w io.Writer, title string, entries []googleads.RegistryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "%s:\n", title); err != nil {
		return err
	}
	for _, entry := range entries {
		if _, err := fmt.Fprintf(w, "  %s %s (%s) %s\n", entry.StoreID, entry.StoreName, entry.Address, entry.ResourceName); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}
