package core

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// Household overview (ADR-0046): inventory value, warranties and the yearly
// power cost of entries that carry price, warranty or power fields.

// HouseholdItem is one entry of the household overview.
type HouseholdItem struct {
	Slug          string  `json:"slug"`
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	Price         float64 `json:"price,omitempty"`
	PurchaseDate  string  `json:"purchaseDate,omitempty"`
	WarrantyUntil string  `json:"warrantyUntil,omitempty"`
	// WarrantyDays is the number of days left (negative: expired).
	WarrantyDays *int    `json:"warrantyDays,omitempty"`
	Power        float64 `json:"power,omitempty"`   // W while in use
	Standby      float64 `json:"standby,omitempty"` // W otherwise
	Hours        float64 `json:"hours,omitempty"`   // hours in use per day
	KWh          float64 `json:"kwh"`               // per year
	Cost         float64 `json:"cost"`              // per year, in the price's currency
}

// Household is the overview of a project.
type Household struct {
	Items       []HouseholdItem `json:"items"`
	Value       float64         `json:"value"`
	KWh         float64         `json:"kwh"`
	Cost        float64         `json:"cost"`
	PricePerKWh float64         `json:"pricePerKwh"` // in currency units (e.g. 0.32 €)
	Expiring    int             `json:"expiring"`    // warranties ending within 60 days
}

const defaultPowerPrice = 0.35

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		f, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(x), ",", "."), 64)
		return f
	}
	return 0
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// yearlyKWh: in use for hours a day, on standby the rest.
func yearlyKWh(power, standby, hours float64) float64 {
	if hours <= 0 && power > 0 && standby == 0 {
		hours = 24 // a device without hours runs all day (router, fridge)
	}
	hours = math.Min(math.Max(hours, 0), 24)
	return (power*hours + standby*(24-hours)) * 365 / 1000
}

func householdItem(e store.Entity, price float64, now time.Time) (HouseholdItem, bool) {
	attrs := map[string]any{}
	_ = json.Unmarshal(e.Attrs, &attrs)
	it := HouseholdItem{Slug: e.Slug, Name: e.Name, Kind: e.Kind, Price: num(attrs["price"]), Power: num(attrs["power"]), Standby: num(attrs["standby"]), Hours: num(attrs["hours_per_day"])}
	it.PurchaseDate, _ = attrs["purchase_date"].(string)
	it.WarrantyUntil, _ = attrs["warranty_until"].(string)
	if e.Kind == "smart_plug" {
		it.Power = 0 // a smart plug's "power" is what it measures, not what it uses
	}
	if it.Price == 0 && it.PurchaseDate == "" && it.WarrantyUntil == "" && it.Power == 0 && it.Standby == 0 {
		return it, false
	}
	if d, err := time.Parse(time.DateOnly, it.WarrantyUntil); err == nil {
		days := int(math.Ceil(d.Sub(now.Truncate(24*time.Hour)).Hours() / 24))
		it.WarrantyDays = &days
	}
	it.KWh = round2(yearlyKWh(it.Power, it.Standby, it.Hours))
	it.Cost = round2(it.KWh * price)
	return it, true
}

func (s *Service) powerPrice(ctx context.Context, projectID string) float64 {
	v, _ := s.DB.Setting(ctx, "power.price:"+projectID, "")
	if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
		return f
	}
	return defaultPowerPrice
}

// Household returns the household overview (readers).
func (s *Service) Household(ctx context.Context, a Actor, ref string) (Household, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return Household{}, err
	}
	ents, err := s.DB.ListEntities(ctx, p.ID, "")
	if err != nil {
		return Household{}, err
	}
	h := Household{Items: []HouseholdItem{}, PricePerKWh: s.powerPrice(ctx, p.ID)}
	now := time.Now().UTC()
	for _, e := range ents {
		it, ok := householdItem(e, h.PricePerKWh, now)
		if !ok {
			continue
		}
		h.Items = append(h.Items, it)
		h.Value += it.Price
		h.KWh += it.KWh
		h.Cost += it.Cost
		if it.WarrantyDays != nil && *it.WarrantyDays >= 0 && *it.WarrantyDays <= 60 {
			h.Expiring++
		}
	}
	h.Value, h.KWh, h.Cost = round2(h.Value), round2(h.KWh), round2(h.Cost)
	sort.SliceStable(h.Items, func(i, j int) bool { return h.Items[i].Cost > h.Items[j].Cost })
	return h, nil
}

// SetPowerPrice sets the electricity price per kWh of a project (editors).
func (s *Service) SetPowerPrice(ctx context.Context, a Actor, ref string, price float64) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	if price <= 0 || price > 10 {
		return invalid("validation.power_price", nil, "the price per kWh must be between 0 and 10")
	}
	if err := s.DB.SetSetting(ctx, "power.price:"+p.ID, strconv.FormatFloat(price, 'f', -1, 64), a.String()); err != nil {
		return err
	}
	s.event(ctx, a, "project.power_price", p.ID, map[string]float64{"price": price})
	return nil
}

// RemindWarranties notifies once, 30 days before a warranty ends, the
// entry's responsible person or else the project owners (System context, daily).
func (s *Service) RemindWarranties(ctx context.Context, now time.Time) {
	sys := store.System(ctx)
	day := now.UTC().Format(time.DateOnly)
	if last, _ := s.DB.Setting(sys, "warranty.day", ""); last == day {
		return
	}
	_ = s.DB.SetSetting(sys, "warranty.day", day, "system")
	tenants, err := s.DB.Tenants(sys)
	if err != nil {
		return
	}
	for _, t := range tenants {
		tctx := store.WithTenant(ctx, t.ID)
		projects, err := s.DB.ListProjects(tctx, "", true)
		if err != nil {
			continue
		}
		for _, p := range projects {
			s.remindWarranties(tctx, p, now)
		}
	}
}

func (s *Service) remindWarranties(ctx context.Context, p store.Project, now time.Time) {
	ents, err := s.DB.ListEntities(ctx, p.ID, "")
	if err != nil {
		return
	}
	var owners []string
	for _, e := range ents {
		it, ok := householdItem(e, 0, now)
		if !ok || it.WarrantyDays == nil || *it.WarrantyDays < 0 || *it.WarrantyDays > 30 {
			continue
		}
		key := "warranty:" + e.ID + ":" + it.WarrantyUntil
		if done, _ := s.DB.Setting(ctx, key, ""); done != "" {
			continue
		}
		_ = s.DB.SetSetting(ctx, key, now.UTC().Format(time.RFC3339), "system")
		to := []string{}
		if m, err := s.DB.GetEntityMeta(ctx, e.ID); err == nil && m.OwnerID != "" {
			to = append(to, m.OwnerID)
		} else {
			if owners == nil {
				members, _ := s.DB.ListMembers(ctx, p.ID)
				for _, m := range members {
					if m.Role == store.RoleOwner {
						owners = append(owners, m.UserID)
					}
				}
			}
			to = owners
		}
		for _, id := range to {
			if u, err := s.DB.UserByID(ctx, id); err == nil && !u.Disabled {
				s.notify(ctx, u, Note{Kind: "entity.warranty", ProjectID: p.ID, Project: p.Name, Subject: key, Title: e.Name,
					Body: it.WarrantyUntil, Link: "/p/" + p.Slug + "/e/" + e.Slug})
			}
		}
	}
}
