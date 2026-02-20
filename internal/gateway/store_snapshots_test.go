package gateway

import "testing"

func TestPlanStoreRestoreAppliesPricingDefaultsForLegacyPlans(t *testing.T) {
	store := NewPlanStore()
	store.Restore(planStoreSnapshot{
		Plans: []Plan{
			{
				ID:            "free",
				Name:          "Free",
				MaxRoutes:     5,
				MaxConnectors: 2,
				MaxRPS:        10,
				MaxMonthlyGB:  10,
				TLSEnabled:    false,
			},
			{
				ID:            "pro",
				Name:          "Pro",
				MaxRoutes:     50,
				MaxConnectors: 10,
				MaxRPS:        100,
				MaxMonthlyGB:  500,
				TLSEnabled:    true,
			},
			{
				ID:            "business",
				Name:          "Business",
				MaxRoutes:     250,
				MaxConnectors: 50,
				MaxRPS:        500,
				MaxMonthlyGB:  5000,
				TLSEnabled:    true,
			},
		},
	})

	free, ok := store.GetPlan("free")
	if !ok {
		t.Fatalf("free plan not found after restore")
	}
	if free.PublicOrder != 1 {
		t.Fatalf("expected free public order 1, got %d", free.PublicOrder)
	}

	pro, ok := store.GetPlan("pro")
	if !ok {
		t.Fatalf("pro plan not found after restore")
	}
	if pro.PriceMonthlyUSD != 20 || pro.PriceAnnualUSD != 200 || pro.PublicOrder != 2 {
		t.Fatalf("pro defaults not applied: %+v", pro)
	}

	business, ok := store.GetPlan("business")
	if !ok {
		t.Fatalf("business plan not found after restore")
	}
	if business.PriceMonthlyUSD != 100 || business.PriceAnnualUSD != 1000 || business.PublicOrder != 3 {
		t.Fatalf("business defaults not applied: %+v", business)
	}
}
