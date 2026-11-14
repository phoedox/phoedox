package core

import (
	"context"
	"testing"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

func TestHousehold(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Home"})
	_ = s.SetMember(ctx, owner, p.Slug, "reader@x.de", store.RoleReader)
	soon := time.Now().UTC().AddDate(0, 0, 20).Format(time.DateOnly)
	for _, in := range []EntityInput{
		{Kind: "appliance", Name: "Fridge", Attrs: map[string]any{"price": 899.0, "warranty_until": soon, "power": 20.0}},
		{Kind: "appliance", Name: "Washer", Attrs: map[string]any{"price": "600", "power": 2000.0, "standby": 1.0, "hours_per_day": 1.0}},
		{Kind: "router", Name: "Router"},
		{Kind: "smart_plug", Name: "Plug", Attrs: map[string]any{"power": 15.0}},
	} {
		if _, err := s.UpsertEntity(ctx, owner, p.ID, in); err != nil {
			t.Fatal(in.Name, err)
		}
	}
	h, err := s.Household(ctx, reader, p.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Items) != 2 || h.Value != 1499 || h.Expiring != 1 || h.PricePerKWh != defaultPowerPrice {
		t.Fatalf("household: %+v", h)
	}
	// Fridge: 20 W all day = 175.2 kWh; washer: 2000 W × 1 h + 1 W × 23 h a day = 738.4 kWh.
	byName := map[string]HouseholdItem{}
	for _, it := range h.Items {
		byName[it.Name] = it
	}
	if byName["Fridge"].KWh != 175.2 || byName["Washer"].KWh != 738.4 || h.Items[0].Name != "Washer" {
		t.Fatalf("energy: %+v", h.Items)
	}
	if d := byName["Fridge"].WarrantyDays; d == nil || *d != 20 {
		t.Fatalf("warranty days: %v", d)
	}
	if err := s.SetPowerPrice(ctx, reader, p.Slug, 0.3); err == nil {
		t.Fatal("reader set the price")
	}
	if err := s.SetPowerPrice(ctx, owner, p.Slug, 0); invalidKey(err) != "validation.power_price" {
		t.Fatalf("bad price: %v", err)
	}
	_ = s.SetPowerPrice(ctx, owner, p.Slug, 0.3)
	if h, _ = s.Household(ctx, reader, p.Slug); h.Cost != round2(913.6*0.3) {
		t.Fatalf("cost: %v", h.Cost)
	}

	// Warranty reminder: once.
	s.RemindWarranties(ctx, time.Now())
	s.RemindWarranties(ctx, time.Now().Add(24*time.Hour))
	n := 0
	list, _ := s.Notifications(ctx, owner, false, 0)
	for _, it := range list.Items {
		if it.Kind == "entity.warranty" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("warranty notes: %d", n)
	}
}
