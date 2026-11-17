package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/payments"
	"github.com/phoedox/phoedox/internal/store"
)

// AI credits (ADR-0080): people and organizations hold lots of credit,
// bought (offers set by the system admin, paid through the payment
// provider, ADR-0079) or granted by the system admin. Each lot has its own
// expiry (never, or a date). When the monthly AI budget is used up, the
// chat goes on and draws the charged amount (in-app prices) from credits:
// the person's own first, then the organization's, the lot that expires
// first first. Amounts are cents like the budgets; lots keep micro-dollars.

const settingCreditOffers = "ai_credits.offers"

// CreditOffer is a pack of credit people or organizations can buy.
type CreditOffer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Price in the smallest unit of the payment currency.
	Price int64 `json:"price"`
	// Credit in cents of in-app prices (like the budgets).
	Credit int64 `json:"credit"`
	// ValidMonths: the credit expires after so many months (0 = never).
	ValidMonths int `json:"validMonths"`
	// For: user, org or both.
	For string `json:"for"`
}

// CreditInfo is the credit of a person or an organization.
type CreditInfo struct {
	// Balance in cents of usable credit.
	Balance int64             `json:"balance"`
	Lots    []store.CreditLot `json:"lots"`
	History []store.CreditLot `json:"history"`
	// Offers to buy (when payments are set up).
	Offers   []CreditOffer `json:"offers"`
	Currency string        `json:"currency"`
}

var offerIDRe = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

func (s *Service) creditOffers(ctx context.Context) []CreditOffer {
	var out []CreditOffer
	if raw, _ := s.DB.Setting(ctx, settingCreditOffers, ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	if out == nil {
		out = []CreditOffer{}
	}
	return out
}

func (s *Service) creditInfo(ctx context.Context, kind, owner string) (CreditInfo, error) {
	now := time.Now()
	bal, err := s.DB.CreditBalance(ctx, kind, owner, now)
	if err != nil {
		return CreditInfo{}, err
	}
	lots, err := s.DB.CreditLots(ctx, kind, owner, false, now)
	if err != nil {
		return CreditInfo{}, err
	}
	hist, err := s.DB.CreditLots(ctx, kind, owner, true, now)
	if err != nil {
		return CreditInfo{}, err
	}
	out := CreditInfo{Balance: centsOf(bal), Lots: lots, History: hist, Offers: []CreditOffer{}}
	c := s.readPaymentConfig(ctx)
	if s.providerFor(c) != nil {
		out.Currency = c.Currency
		for _, o := range s.creditOffers(ctx) {
			if o.For == "both" || o.For == kind {
				out.Offers = append(out.Offers, o)
			}
		}
	}
	return out, nil
}

// MyCredits is the person's own credit.
func (s *Service) MyCredits(ctx context.Context, a Actor) (CreditInfo, error) {
	if err := s.fullUser(a); err != nil {
		return CreditInfo{}, err
	}
	return s.creditInfo(ctx, "user", a.User.ID)
}

// OrgCredits is an organization's credit (its admins).
func (s *Service) OrgCredits(ctx context.Context, a Actor, ref string) (CreditInfo, error) {
	if err := s.fullUser(a); err != nil {
		return CreditInfo{}, err
	}
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return CreditInfo{}, err
	}
	return s.creditInfo(ctx, "org", o.ID)
}

// BuyCredits opens the checkout of an offer, for the person (orgRef "") or
// an organization (its admins).
func (s *Service) BuyCredits(ctx context.Context, a Actor, orgRef, offerID string) (PurchaseCheckout, error) {
	if err := s.fullUser(a); err != nil {
		return PurchaseCheckout{}, err
	}
	kind, orgID, back := "user", "", "/settings/billing"
	if orgRef != "" {
		o, err := s.org(ctx, a, orgRef, store.OrgAdmin)
		if err != nil {
			return PurchaseCheckout{}, err
		}
		kind, orgID, back = "org", o.ID, "/orgs/"+o.Slug
	}
	for _, o := range s.creditOffers(ctx) {
		if o.ID != offerID || (o.For != "both" && o.For != kind) {
			continue
		}
		desc := "AI credits"
		if o.ValidMonths > 0 {
			desc += ", valid for " + itoa(o.ValidMonths) + " months"
		}
		return s.StartPurchase(ctx, a, PurchaseInput{Kind: "ai_credits", Item: o.ID, Name: o.Name, Description: desc, Amount: o.Price, OrgID: orgID, ReturnPath: back})
	}
	return PurchaseCheckout{}, invalid("validation.credits_offer", nil, "unknown offer")
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// creditsPaid adds the lot of a paid purchase and empties it on a refund.
func (s *Service) creditsPaid(ctx context.Context, p store.Purchase, ev payments.Event) {
	switch ev.Type {
	case payments.EventPaid:
		if _, err := s.DB.CreditLotBySource(ctx, "purchase", p.ID); err == nil {
			return // already added
		}
		var offer *CreditOffer
		for _, o := range s.creditOffers(ctx) {
			if o.ID == p.Item {
				offer = &o
				break
			}
		}
		if offer == nil {
			slog.Error("paid AI credits of an unknown offer", "purchase", p.ID, "offer", p.Item)
			return
		}
		kind, owner := "user", p.UserID
		if p.OrgID != "" {
			kind, owner = "org", p.OrgID
		}
		now := store.Now()
		lot := store.CreditLot{ID: newID(), OwnerKind: kind, OwnerID: owner, Amount: offer.Credit * 10_000, Remaining: offer.Credit * 10_000,
			Source: "purchase", SourceRef: p.ID, Note: offer.Name, CreatedBy: p.UserID, CreatedAt: now}
		if offer.ValidMonths > 0 {
			exp := store.Time{Time: now.AddDate(0, offer.ValidMonths, 0)}
			lot.ExpiresAt = &exp
		}
		if err := s.DB.AddCreditLot(ctx, lot); err != nil {
			slog.Error("add AI credits", "purchase", p.ID, "err", err)
			return
		}
		s.event(ctx, Actor{Via: "system"}, "ai_credits.purchase", owner, map[string]any{"purchase": p.ID, "credit": offer.Credit, "expires": lot.ExpiresAt})
	case payments.EventRefunded:
		if lot, err := s.DB.CreditLotBySource(ctx, "purchase", p.ID); err == nil {
			_ = s.DB.EmptyCreditLot(ctx, lot.ID)
			s.event(ctx, Actor{Via: "system"}, "ai_credits.refund", lot.OwnerID, map[string]any{"purchase": p.ID, "left": centsOf(lot.Remaining)})
		}
	}
}

// creditsAvailable is the usable credit for a call beyond the budget:
// the person's own, then the organization's.
func (s *Service) creditsAvailable(ctx context.Context, userID, orgID string) int64 {
	now := time.Now()
	n, _ := s.DB.CreditBalance(ctx, "user", userID, now)
	if orgID != "" {
		o, _ := s.DB.CreditBalance(ctx, "org", orgID, now)
		n += o
	}
	return n
}

// useCredits takes the charge of a call from credits and returns what was taken.
func (s *Service) useCredits(ctx context.Context, userID, orgID string, charge int64) int64 {
	now := time.Now()
	took, err := s.DB.UseCredit(ctx, "user", userID, charge, now)
	if err != nil {
		slog.Warn("use AI credits", "err", err)
	}
	if took < charge && orgID != "" {
		more, err := s.DB.UseCredit(ctx, "org", orgID, charge-took, now)
		if err != nil {
			slog.Warn("use AI credits", "err", err)
		}
		took += more
	}
	return took
}

// --- administration ----------------------------------------------------------

// CreditAdmin is what the administration shows.
type CreditAdmin struct {
	Offers        []CreditOffer     `json:"offers"`
	Grants        []store.CreditLot `json:"grants"`
	PaymentsReady bool              `json:"paymentsReady"`
	Currency      string            `json:"currency"`
}

// AdminCredits lists the offers and the latest grants (system admins).
func (s *Service) AdminCredits(ctx context.Context, a Actor) (CreditAdmin, error) {
	if err := s.adminOnly(a); err != nil {
		return CreditAdmin{}, err
	}
	grants, err := s.DB.RecentGrants(ctx, 100)
	if err != nil {
		return CreditAdmin{}, err
	}
	c := s.readPaymentConfig(ctx)
	return CreditAdmin{Offers: s.creditOffers(ctx), Grants: grants, PaymentsReady: s.providerFor(c) != nil, Currency: c.Currency}, nil
}

// SetCreditOffers replaces the offers (system admins).
func (s *Service) SetCreditOffers(ctx context.Context, a Actor, offers []CreditOffer) ([]CreditOffer, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range offers {
		o := &offers[i]
		o.ID, o.Name = strings.TrimSpace(o.ID), strings.TrimSpace(o.Name)
		if o.For == "" {
			o.For = "both"
		}
		if !offerIDRe.MatchString(o.ID) || seen[o.ID] || o.Name == "" || len(o.Name) > 100 || o.Price <= 0 || o.Credit <= 0 || o.Credit > 100_000_000 ||
			o.ValidMonths < 0 || o.ValidMonths > 120 || !slices.Contains([]string{"user", "org", "both"}, o.For) {
			return nil, invalid("validation.credits_offer_invalid", map[string]any{"n": i + 1}, "offer %d: id (a-z, 0-9, -), name, price, credit, months 0–120", i+1)
		}
		seen[o.ID] = true
	}
	raw, _ := json.Marshal(offers)
	if err := s.DB.SetSetting(ctx, settingCreditOffers, string(raw), a.User.ID); err != nil {
		return nil, err
	}
	s.event(ctx, a, "ai_credits.offers", "instance", map[string]any{"offers": len(offers)})
	return offers, nil
}

// CreditGrantInput hands out credit (system admins).
type CreditGrantInput struct {
	// Target: users or orgs.
	Target string `json:"target"`
	// Filter: All, or organizations (slugs; for users: their members),
	// e-mail addresses (users) and the sign-up period (users, YYYY-MM-DD).
	All            bool     `json:"all"`
	Orgs           []string `json:"orgs,omitempty"`
	Emails         []string `json:"emails,omitempty"`
	SignedUpAfter  string   `json:"signedUpAfter,omitempty"`
	SignedUpBefore string   `json:"signedUpBefore,omitempty"`
	// Credit in cents; ExpiresAt YYYY-MM-DD or "" (never).
	Credit    int64  `json:"credit"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	Note      string `json:"note,omitempty"`
	// DryRun only counts the recipients.
	DryRun bool `json:"dryRun"`
}

// CreditGrantResult tells who gets (or got) the credit.
type CreditGrantResult struct {
	Count int      `json:"count"`
	Names []string `json:"names"`
}

type grantee struct{ kind, id, name, slug string }

func (s *Service) grantees(ctx context.Context, in CreditGrantInput) ([]grantee, error) {
	day := func(v string) (time.Time, error) {
		if v == "" {
			return time.Time{}, nil
		}
		return time.Parse("2006-01-02", v)
	}
	after, err1 := day(in.SignedUpAfter)
	before, err2 := day(in.SignedUpBefore)
	if err1 != nil || err2 != nil {
		return nil, invalid("validation.credits_filter", nil, "dates look like 2026-11-17")
	}
	orgIDs := map[string]bool{}
	for _, ref := range in.Orgs {
		if ref = strings.TrimSpace(ref); ref == "" {
			continue
		}
		o, err := s.DB.OrgFor(ctx, ref, "")
		if err != nil {
			return nil, invalid("validation.org_unknown", map[string]any{"org": ref}, "organization %q not found", ref)
		}
		orgIDs[o.ID] = true
	}
	emails := map[string]bool{}
	for _, e := range in.Emails {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			emails[e] = true
		}
	}
	if !in.All && len(orgIDs) == 0 && len(emails) == 0 && after.IsZero() && before.IsZero() {
		return nil, invalid("validation.credits_filter_empty", nil, "choose everyone or a filter")
	}
	var out []grantee
	switch in.Target {
	case "orgs":
		orgs, err := s.DB.ListOrgs(ctx, "", true)
		if err != nil {
			return nil, err
		}
		for _, o := range orgs {
			if in.All || orgIDs[o.ID] {
				out = append(out, grantee{"org", o.ID, o.Name, o.Slug})
			}
		}
	case "users":
		members := map[string]bool{}
		for id := range orgIDs {
			ms, _ := s.DB.OrgMembers(ctx, id)
			for _, m := range ms {
				members[m.UserID] = true
			}
		}
		users, err := s.DB.ListUsers(ctx)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			if u.Disabled || u.Email == "" {
				continue
			}
			// Everyone, or members of the organizations and the listed
			// addresses (each only when given); the sign-up period narrows both.
			ok := in.All || (len(orgIDs) == 0 || members[u.ID]) && (len(emails) == 0 || emails[strings.ToLower(u.Email)])
			if !after.IsZero() && u.CreatedAt.Before(after) || !before.IsZero() && !u.CreatedAt.Before(before.AddDate(0, 0, 1)) {
				ok = false
			}
			if ok {
				out = append(out, grantee{"user", u.ID, u.Name, ""})
			}
		}
	default:
		return nil, invalid("validation.credits_target", nil, "target must be users or orgs")
	}
	return out, nil
}

// GrantCredits hands out credit to everyone or to those matching a filter
// (system admins); DryRun only counts them.
func (s *Service) GrantCredits(ctx context.Context, a Actor, in CreditGrantInput) (CreditGrantResult, error) {
	if err := s.adminOnly(a); err != nil {
		return CreditGrantResult{}, err
	}
	if in.Credit <= 0 || in.Credit > 100_000_000 {
		return CreditGrantResult{}, invalid("validation.credits_amount", nil, "the credit must be between 1 and 100000000 cents")
	}
	var exp *store.Time
	if v := strings.TrimSpace(in.ExpiresAt); v != "" {
		d, err := time.Parse("2006-01-02", v)
		if err != nil || !d.After(time.Now()) {
			return CreditGrantResult{}, invalid("validation.credits_expiry", nil, "the expiry must be a future day (YYYY-MM-DD)")
		}
		e := store.Time{Time: d.Add(24*time.Hour - time.Second)} // to the end of that day
		exp = &e
	}
	list, err := s.grantees(ctx, in)
	if err != nil {
		return CreditGrantResult{}, err
	}
	out := CreditGrantResult{Count: len(list), Names: []string{}}
	for i, g := range list {
		if i < 20 {
			out.Names = append(out.Names, g.name)
		}
	}
	if in.DryRun || len(list) == 0 {
		return out, nil
	}
	grant := newID()
	note := excerpt(strings.TrimSpace(in.Note), 300)
	now := store.Now()
	for _, g := range list {
		lot := store.CreditLot{ID: newID(), OwnerKind: g.kind, OwnerID: g.id, Amount: in.Credit * 10_000, Remaining: in.Credit * 10_000, ExpiresAt: exp,
			Source: "grant", SourceRef: grant, Note: note, CreatedBy: a.User.ID, CreatedAt: now}
		if err := s.DB.AddCreditLot(ctx, lot); err != nil {
			return out, err
		}
		if g.kind == "user" {
			if u, err := s.DB.UserByID(ctx, g.id); err == nil {
				s.notify(ctx, u, Note{Kind: "ai_credits.granted", Subject: lot.ID, Title: note, Body: note, Link: "/settings/billing", Actor: a.User.Name})
			}
		} else {
			s.notifyOrgAdmins(ctx, g.id, Note{Kind: "ai_credits.granted", Subject: lot.ID, Title: note, Body: note, Link: "/orgs/" + g.slug, Actor: a.User.Name})
		}
	}
	s.event(ctx, a, "ai_credits.grant", grant, map[string]any{"target": in.Target, "count": len(list), "credit": in.Credit, "expires": in.ExpiresAt, "note": note})
	return out, nil
}
