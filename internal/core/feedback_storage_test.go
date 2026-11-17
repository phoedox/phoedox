package core

import (
	"context"
	"testing"

	"github.com/phoedox/phoedox/internal/payments"
	"github.com/phoedox/phoedox/internal/store"
)

func TestFeedbackStoragePacks(t *testing.T) {
	ctx := context.Background()
	s, owner, reader, _ := setup(t)
	admin := owner
	admin.User.IsAdmin = true
	p, _ := s.CreateProject(ctx, owner, ProjectInput{Name: "Game"})
	_ = s.SetMember(ctx, owner, p.Slug, reader.User.Email, store.RoleEditor)

	if _, err := s.SetFeedbackStorageOffers(ctx, owner, []StorageOffer{{ID: "a", Name: "A", Price: 100, MB: 5}}); err != ErrForbidden {
		t.Fatalf("non-admin set offers: %v", err)
	}
	if _, err := s.SetFeedbackStorageOffers(ctx, admin, []StorageOffer{{ID: "Bad ID", Name: "x", Price: 1, MB: 1}}); invalidKey(err) != "validation.storage_offer_invalid" {
		t.Fatalf("bad offer: %v", err)
	}
	if _, err := s.SetFeedbackStorageOffers(ctx, admin, []StorageOffer{{ID: "plus-5g", Name: "+5 GB for a year", Price: 900, MB: 5120, ValidMonths: 12}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectFeedbackStorage(ctx, reader, p.Slug); err == nil {
		t.Fatal("editor sees the owner's storage page")
	}
	st, err := s.ProjectFeedbackStorage(ctx, owner, p.Slug)
	if err != nil || st.BaseMB != 1024 || st.LimitBytes != 1024<<20 || len(st.Offers) != 0 {
		t.Fatalf("storage (no payments set up, no offers shown): %v %+v", err, st)
	}
	// A paid purchase adds the pack, a refund ends it.
	pur := store.Purchase{ID: "pur1", UserID: owner.User.ID, Kind: "feedback_storage", Item: "plus-5g@" + p.ID}
	s.feedbackStoragePaid(ctx, pur, payments.Event{Type: payments.EventPaid})
	s.feedbackStoragePaid(ctx, pur, payments.Event{Type: payments.EventPaid}) // webhooks repeat
	st, _ = s.ProjectFeedbackStorage(ctx, owner, p.Slug)
	if st.ExtraMB != 5120 || st.LimitBytes != int64(1024+5120)<<20 || len(st.Lots) != 1 || st.Lots[0].ExpiresAt == nil {
		t.Fatalf("after purchase: %+v", st)
	}
	s.feedbackStoragePaid(ctx, pur, payments.Event{Type: payments.EventRefunded})
	st, _ = s.ProjectFeedbackStorage(ctx, owner, p.Slug)
	if st.ExtraMB != 0 || st.LimitBytes != 1024<<20 || !st.Lots[0].Revoked {
		t.Fatalf("after refund: %+v", st)
	}
	if _, err := s.BuyFeedbackStorage(ctx, owner, p.Slug, "nope"); invalidKey(err) != "validation.storage_offer" {
		t.Fatalf("unknown offer: %v", err)
	}
}
