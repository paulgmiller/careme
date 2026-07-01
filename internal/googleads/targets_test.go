package googleads

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanSyncCreatesMissingAndRemovesOnlyManagedStaleEntries(t *testing.T) {
	targets := []Target{
		{StoreID: "11111111", LatMicro: 47000000, LonMicro: -122000000, RadiusMiles: 2},
		{StoreID: "22222222", LatMicro: 48000000, LonMicro: -123000000, RadiusMiles: 2},
		{StoreID: "33333333", LatMicro: 49000000, LonMicro: -124000000, RadiusMiles: 2},
	}
	registry := []RegistryEntry{
		{StoreID: "11111111", ResourceName: "customers/1/campaignCriteria/10~1"},
		{StoreID: "99999999", ResourceName: "customers/1/campaignCriteria/10~9"},
	}
	existing := []ProximityCriterion{
		{ResourceName: "customers/1/campaignCriteria/10~1", LatMicro: 47000000, LonMicro: -122000000, RadiusMiles: 2},
		{ResourceName: "customers/1/campaignCriteria/10~2", LatMicro: 48000000, LonMicro: -123000000, RadiusMiles: 2},
		{ResourceName: "customers/1/campaignCriteria/10~9", LatMicro: 50000000, LonMicro: -125000000, RadiusMiles: 2},
	}

	plan := PlanSync(targets, registry, existing)

	require.Len(t, plan.Keep, 1)
	assert.Equal(t, "11111111", plan.Keep[0].StoreID)
	require.Len(t, plan.Skip, 1)
	assert.Equal(t, "22222222", plan.Skip[0].StoreID)
	require.Len(t, plan.Create, 1)
	assert.Equal(t, "33333333", plan.Create[0].StoreID)
	require.Len(t, plan.Remove, 1)
	assert.Equal(t, "99999999", plan.Remove[0].StoreID)
}

func TestPlanSyncForgetsMissingManagedCriterionAndRecreatesRequestedTarget(t *testing.T) {
	targets := []Target{
		{StoreID: "11111111", LatMicro: 47000000, LonMicro: -122000000, RadiusMiles: 2},
	}
	registry := []RegistryEntry{
		{StoreID: "11111111", ResourceName: "customers/1/campaignCriteria/10~1"},
		{StoreID: "22222222", ResourceName: "customers/1/campaignCriteria/10~2"},
	}

	plan := PlanSync(targets, registry, nil)

	require.Len(t, plan.Create, 1)
	assert.Equal(t, "11111111", plan.Create[0].StoreID)
	require.Len(t, plan.Forget, 2)
	assert.Equal(t, "11111111", plan.Forget[0].StoreID)
	assert.Equal(t, "22222222", plan.Forget[1].StoreID)
	assert.Empty(t, plan.Remove)
	assert.Empty(t, plan.Keep)
}

func TestApplyCreatedEntriesRequiresOneResourcePerTarget(t *testing.T) {
	_, err := ApplyCreatedEntries(Registry{}, []Target{{StoreID: "1"}}, nil)
	require.Error(t, err)
}

func TestApplyCreatedEntriesUpsertsAndSortsRegistry(t *testing.T) {
	registry, err := ApplyCreatedEntries(
		Registry{Entries: []RegistryEntry{{StoreID: "2", ResourceName: "old"}}},
		[]Target{{StoreID: "1", StoreName: "Store 1", LatMicro: 1, LonMicro: 2, RadiusMiles: 2}},
		[]string{"new"},
	)
	require.NoError(t, err)

	require.Len(t, registry.Entries, 2)
	assert.Equal(t, "1", registry.Entries[0].StoreID)
	assert.Equal(t, "new", registry.Entries[0].ResourceName)
	assert.Equal(t, "2", registry.Entries[1].StoreID)
}
