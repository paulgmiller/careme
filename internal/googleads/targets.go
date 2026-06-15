package googleads

import (
	"math"
	"sort"
	"strings"
)

type Target struct {
	StoreID     string
	StoreName   string
	Address     string
	LatMicro    int64
	LonMicro    int64
	RadiusMiles float64
	FinalURL    string
}

type RegistryEntry struct {
	StoreID      string  `json:"store_id"`
	StoreName    string  `json:"store_name"`
	Address      string  `json:"address"`
	LatMicro     int64   `json:"lat_micro"`
	LonMicro     int64   `json:"lon_micro"`
	RadiusMiles  float64 `json:"radius_miles"`
	ResourceName string  `json:"resource_name"`
	FinalURL     string  `json:"final_url,omitempty"`
	AdGroup      string  `json:"ad_group,omitempty"`
	AdGroupAd    string  `json:"ad_group_ad,omitempty"`
}

type Plan struct {
	Create []Target
	Remove []RegistryEntry
	Forget []RegistryEntry
	Keep   []RegistryEntry
	Skip   []Target
}

func PlanSync(targets []Target, registry []RegistryEntry, existing []ProximityCriterion) Plan {
	registryByStore := make(map[string]RegistryEntry, len(registry))
	for _, entry := range registry {
		if strings.TrimSpace(entry.StoreID) == "" || strings.TrimSpace(entry.ResourceName) == "" {
			continue
		}
		registryByStore[entry.StoreID] = entry
	}

	existingByShape := make(map[string]struct{}, len(existing))
	existingByResourceName := make(map[string]struct{}, len(existing))
	for _, criterion := range existing {
		existingByShape[targetShape(criterion.LatMicro, criterion.LonMicro, criterion.RadiusMiles)] = struct{}{}
		if strings.TrimSpace(criterion.ResourceName) != "" {
			existingByResourceName[criterion.ResourceName] = struct{}{}
		}
	}

	seenTargets := make(map[string]struct{}, len(targets))
	plan := Plan{}
	for _, target := range targets {
		seenTargets[target.StoreID] = struct{}{}
		if entry, ok := registryByStore[target.StoreID]; ok {
			if _, exists := existingByResourceName[entry.ResourceName]; exists {
				plan.Keep = append(plan.Keep, entry)
				continue
			}
			plan.Forget = append(plan.Forget, entry)
			if _, ok := existingByShape[targetShape(target.LatMicro, target.LonMicro, target.RadiusMiles)]; ok {
				plan.Skip = append(plan.Skip, target)
				continue
			}
			plan.Create = append(plan.Create, target)
			continue
		}
		if _, ok := existingByShape[targetShape(target.LatMicro, target.LonMicro, target.RadiusMiles)]; ok {
			plan.Skip = append(plan.Skip, target)
			continue
		}
		plan.Create = append(plan.Create, target)
	}

	for _, entry := range registry {
		if _, ok := seenTargets[entry.StoreID]; ok {
			continue
		}
		if strings.TrimSpace(entry.ResourceName) == "" {
			continue
		}
		if _, exists := existingByResourceName[entry.ResourceName]; exists {
			plan.Remove = append(plan.Remove, entry)
			continue
		}
		plan.Forget = append(plan.Forget, entry)
	}

	sortTargets(plan.Create)
	sortTargets(plan.Skip)
	sortEntries(plan.Forget)
	sortEntries(plan.Keep)
	sortEntries(plan.Remove)
	return plan
}

func MicroDegrees(v float64) int64 {
	return int64(math.Round(v * 1_000_000))
}

func targetShape(lat, lon int64, radius float64) string {
	return strings.Join([]string{
		formatInt(lat),
		formatInt(lon),
		formatFloat(radius),
	}, "|")
}

func formatInt(v int64) string {
	return strconvFormatInt(v)
}

func formatFloat(v float64) string {
	return strconvFormatFloat(v)
}

func sortTargets(targets []Target) {
	sort.Slice(targets, func(i, j int) bool {
		return targets[i].StoreID < targets[j].StoreID
	})
}

func sortEntries(entries []RegistryEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].StoreID < entries[j].StoreID
	})
}
