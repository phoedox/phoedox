package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/payments"
	"github.com/phoedox/phoedox/internal/store"
)

// More feedback storage: system admins offer packs (MB for some months,
// price); project owners buy them on top of the project's limit. A refund
// ends the pack. Packs only add to a limit: projects without a limit need
// none.

const settingFeedbackStorageOffers = "feedback.storage_offers"

// StorageOffer is a pack of feedback storage.
type StorageOffer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Price int64  `json:"price"` // smallest currency unit
	MB    int    `json:"mb"`
	// ValidMonths: the pack ends after so many months (0 = never).
	ValidMonths int `json:"validMonths"`
}

// FeedbackStorage is a project's feedback storage for its owners.
type FeedbackStorage struct {
	UsedBytes int64 `json:"usedBytes"`
	// LimitBytes is the whole limit (0 = none); BaseMB the project's own
	// limit, ExtraMB what packs add now.
	LimitBytes int64                      `json:"limitBytes"`
	BaseMB     int                        `json:"baseMb"`
	ExtraMB    int                        `json:"extraMb"`
	Lots       []store.FeedbackStorageLot `json:"lots"`
	Offers     []StorageOffer             `json:"offers"`
	Currency   string                     `json:"currency,omitempty"`
}

func (s *Service) storageOffers(ctx context.Context) []StorageOffer {
	var out []StorageOffer
	if raw, _ := s.DB.Setting(ctx, settingFeedbackStorageOffers, ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	if out == nil {
		out = []StorageOffer{}
	}
	return out
}

// feedbackBaseMB is a project's own feedback storage limit (0 = none).
func (s *Service) feedbackBaseMB(ctx context.Context, p store.Project) int {
	mb := s.intSetting(ctx, settingFeedbackQuota, 1024)
	if own, err := s.DB.FeedbackQuota(ctx, p.ID); err == nil && own != nil {
		mb = *own
	}
	return mb
}

// ProjectFeedbackStorage shows usage, packs and offers (owners).
func (s *Service) ProjectFeedbackStorage(ctx context.Context, a Actor, ref string) (FeedbackStorage, error) {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return FeedbackStorage{}, err
	}
	out := FeedbackStorage{BaseMB: s.feedbackBaseMB(ctx, p), LimitBytes: s.feedbackQuotaBytes(ctx, p), Offers: []StorageOffer{}}
	out.UsedBytes, _ = s.DB.FeedbackUsage(ctx, p.ID)
	out.ExtraMB, _ = s.DB.FeedbackExtraMB(ctx, p.ID, time.Now())
	if out.Lots, err = s.DB.FeedbackStorageLots(ctx, p.ID); err != nil {
		return out, err
	}
	c := s.readPaymentConfig(ctx)
	if out.BaseMB > 0 && s.providerFor(c) != nil {
		out.Offers, out.Currency = s.storageOffers(ctx), c.Currency
	}
	return out, nil
}

// BuyFeedbackStorage opens the checkout of a pack for a project (owners).
func (s *Service) BuyFeedbackStorage(ctx context.Context, a Actor, ref, offerID string) (PurchaseCheckout, error) {
	p, err := s.feedbackProject(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return PurchaseCheckout{}, err
	}
	for _, o := range s.storageOffers(ctx) {
		if o.ID != offerID {
			continue
		}
		desc := "Feedback storage for " + p.Name
		if o.ValidMonths > 0 {
			desc += ", " + itoa(o.ValidMonths) + " months"
		}
		return s.StartPurchase(ctx, a, PurchaseInput{Kind: "feedback_storage", Item: o.ID + "@" + p.ID, Name: o.Name, Description: desc, Amount: o.Price,
			OrgID: p.OrgID, ReturnPath: "/p/" + p.Slug + "/settings/feedback"})
	}
	return PurchaseCheckout{}, invalid("validation.storage_offer", nil, "unknown offer")
}

// feedbackStoragePaid adds the pack of a paid purchase and ends it on a refund.
func (s *Service) feedbackStoragePaid(ctx context.Context, p store.Purchase, ev payments.Event) {
	switch ev.Type {
	case payments.EventPaid:
		if _, err := s.DB.FeedbackStorageLotBySource(ctx, "purchase", p.ID); err == nil {
			return // already added
		}
		offerID, projectID, ok := strings.Cut(p.Item, "@")
		if !ok {
			return
		}
		var offer *StorageOffer
		for _, o := range s.storageOffers(ctx) {
			if o.ID == offerID {
				offer = &o
				break
			}
		}
		if offer == nil {
			slog.Error("paid feedback storage of an unknown offer", "purchase", p.ID, "offer", offerID)
			return
		}
		now := store.Now()
		lot := store.FeedbackStorageLot{ID: newID(), ProjectID: projectID, MB: offer.MB, Source: "purchase", SourceRef: p.ID, Note: offer.Name, CreatedBy: p.UserID, CreatedAt: now}
		if offer.ValidMonths > 0 {
			exp := store.Time{Time: now.AddDate(0, offer.ValidMonths, 0)}
			lot.ExpiresAt = &exp
		}
		if err := s.DB.AddFeedbackStorageLot(ctx, lot); err != nil {
			slog.Error("add feedback storage", "purchase", p.ID, "err", err)
			return
		}
		s.event(ctx, Actor{Via: "system"}, "feedback.storage_purchase", projectID, map[string]any{"purchase": p.ID, "mb": offer.MB, "expires": lot.ExpiresAt})
	case payments.EventRefunded:
		if lot, err := s.DB.FeedbackStorageLotBySource(ctx, "purchase", p.ID); err == nil {
			_ = s.DB.RevokeFeedbackStorageLot(ctx, lot.ID)
			s.event(ctx, Actor{Via: "system"}, "feedback.storage_refund", lot.ProjectID, map[string]any{"purchase": p.ID})
		}
	}
}

// FeedbackStorageOffers returns the offers (system admins).
func (s *Service) FeedbackStorageOffers(ctx context.Context, a Actor) ([]StorageOffer, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	return s.storageOffers(ctx), nil
}

// SetFeedbackStorageOffers replaces the offers (system admins).
func (s *Service) SetFeedbackStorageOffers(ctx context.Context, a Actor, offers []StorageOffer) ([]StorageOffer, error) {
	if err := s.adminOnly(a); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range offers {
		o := &offers[i]
		o.ID, o.Name = strings.TrimSpace(o.ID), strings.TrimSpace(o.Name)
		if !offerIDRe.MatchString(o.ID) || seen[o.ID] || o.Name == "" || len(o.Name) > 100 || o.Price <= 0 || o.MB <= 0 || o.MB > 10_000_000 || o.ValidMonths < 0 || o.ValidMonths > 120 {
			return nil, invalid("validation.storage_offer_invalid", map[string]any{"n": i + 1}, "offer %d: id (a-z, 0-9, -), name, price, MB, months 0–120", i+1)
		}
		seen[o.ID] = true
	}
	if offers == nil {
		offers = []StorageOffer{}
	}
	raw, _ := json.Marshal(offers)
	if err := s.DB.SetSetting(ctx, settingFeedbackStorageOffers, string(raw), a.User.ID); err != nil {
		return nil, err
	}
	s.event(ctx, a, "feedback.storage_offers", "instance", map[string]any{"offers": len(offers)})
	return offers, nil
}
