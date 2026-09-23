package billing

import "testing"

func TestSelfHostedCatalogHasNoCommercialLimits(t *testing.T) {
	all := Plans()
	if len(all) != 1 || all[0].ID != "self_hosted" {
		t.Fatalf("plans=%v", all)
	}
	plan := PlanOrDefault("trial")
	if plan.ID != DefaultPlanID || plan.MaxMailboxes != 0 || plan.MaxStorageBytes != 0 || plan.MaxDailySend != 0 || plan.PriceCentsPerUser != 0 {
		t.Fatalf("unexpected self-hosted limits: %+v", plan)
	}
	if !plan.OnlyOfficeEdit || !plan.WikiPublicLinks || !plan.IsActive {
		t.Fatalf("core features disabled: %+v", plan)
	}
}
