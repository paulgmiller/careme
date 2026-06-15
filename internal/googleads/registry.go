package googleads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"careme/internal/cache"
)

const registryPrefix = "google-ads/kroger-proximity/"

type Registry struct {
	CustomerID string          `json:"customer_id"`
	CampaignID string          `json:"campaign_id"`
	UpdatedAt  time.Time       `json:"updated_at"`
	Entries    []RegistryEntry `json:"entries"`
}

func RegistryCacheKey(customerID, campaignID string) string {
	return registryPrefix + sanitizeID(customerID) + "/" + sanitizeID(campaignID) + ".json"
}

func LoadRegistry(ctx context.Context, c cache.Cache, customerID, campaignID string) (Registry, error) {
	registry := Registry{
		CustomerID: customerID,
		CampaignID: campaignID,
	}
	body, err := c.Get(ctx, RegistryCacheKey(customerID, campaignID))
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			return registry, nil
		}
		return Registry{}, err
	}
	defer func() {
		_ = body.Close()
	}()
	raw, err := io.ReadAll(body)
	if err != nil {
		return Registry{}, err
	}
	if err := json.Unmarshal(raw, &registry); err != nil {
		return Registry{}, fmt.Errorf("decode Google Ads registry: %w", err)
	}
	return registry, nil
}

func SaveRegistry(ctx context.Context, c cache.Cache, registry Registry) error {
	registry.UpdatedAt = time.Now().UTC()
	raw, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	return c.Put(ctx, RegistryCacheKey(registry.CustomerID, registry.CampaignID), string(raw), cache.Unconditional())
}

func ApplyCreatedEntries(registry Registry, targets []Target, resourceNames []string) (Registry, error) {
	if len(targets) != len(resourceNames) {
		return Registry{}, fmt.Errorf("created %d criteria for %d targets", len(resourceNames), len(targets))
	}

	entriesByStore := make(map[string]RegistryEntry, len(registry.Entries)+len(targets))
	for _, entry := range registry.Entries {
		entriesByStore[entry.StoreID] = entry
	}
	for i, target := range targets {
		entriesByStore[target.StoreID] = RegistryEntry{
			StoreID:      target.StoreID,
			StoreName:    target.StoreName,
			Address:      target.Address,
			LatMicro:     target.LatMicro,
			LonMicro:     target.LonMicro,
			RadiusMiles:  target.RadiusMiles,
			ResourceName: resourceNames[i],
		}
	}

	registry.Entries = registry.Entries[:0]
	for _, entry := range entriesByStore {
		registry.Entries = append(registry.Entries, entry)
	}
	sortEntries(registry.Entries)
	return registry, nil
}

func RemoveEntries(registry Registry, remove []RegistryEntry) Registry {
	removeByStore := make(map[string]struct{}, len(remove))
	for _, entry := range remove {
		removeByStore[entry.StoreID] = struct{}{}
	}
	entries := make([]RegistryEntry, 0, len(registry.Entries))
	for _, entry := range registry.Entries {
		if _, ok := removeByStore[entry.StoreID]; ok {
			continue
		}
		entries = append(entries, entry)
	}
	registry.Entries = entries
	return registry
}

func sanitizeID(id string) string {
	id = strings.TrimSpace(strings.ReplaceAll(id, "-", ""))
	id = strings.ReplaceAll(id, "/", "_")
	return id
}
