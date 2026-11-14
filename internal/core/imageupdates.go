package core

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/phoedox/phoedox/internal/registry"
	"github.com/phoedox/phoedox/internal/store"
)

// Image updates: agents report the registry digest of each container's
// image; the instance asks the registry for the tag's current digest (the
// agent never talks to registries). A difference means a newer image is
// available; "Update" (container.update) pulls it.

const imageCheckEvery = 6 * time.Hour

// CheckImageUpdates compares container images with their registries
// (all tenants; each image is asked for once per run).
func (s *Service) CheckImageUpdates(ctx context.Context, now time.Time) {
	tenants, err := s.DB.Tenants(ctx)
	if err != nil {
		slog.Warn("image updates", "err", err)
		return
	}
	seen := map[string]bool{}
	latest := map[string]string{} // image → digest ("" = could not ask)
	for _, t := range tenants {
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		tctx := store.WithTenant(ctx, t.ID)
		ents, err := s.DB.TenantEntitiesOfKind(tctx, "container")
		if err != nil {
			slog.Warn("image updates", "tenant", t.ID, "err", err)
			continue
		}
		for _, e := range ents {
			s.checkImage(tctx, e, now, latest)
		}
	}
}

func (s *Service) checkImage(ctx context.Context, e store.Entity, now time.Time, latest map[string]string) {
	attrs := map[string]any{}
	_ = json.Unmarshal(e.Attrs, &attrs)
	image, _ := attrs["image"].(string)
	digest, _ := attrs["image_digest"].(string)
	if image == "" || digest == "" {
		return
	}
	if at, ok := attrs["image_checked"].(string); ok {
		if t, err := time.Parse(time.RFC3339, at); err == nil && now.Sub(t) < imageCheckEvery-10*time.Minute {
			return
		}
	}
	d, asked := latest[image]
	if !asked {
		if ref, err := registry.Parse(image); err == nil {
			rctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			d, err = registry.Digest(rctx, s.RequestPolicy.Client(), ref)
			cancel()
			if err != nil {
				slog.Debug("image update check", "image", image, "err", err)
				d = ""
			}
		}
		latest[image] = d
	}
	if d == "" {
		return
	}
	was, _ := attrs["image_update"].(bool)
	attrs["image_latest"] = d
	attrs["image_update"] = d != digest
	attrs["image_checked"] = now.UTC().Format(time.RFC3339)
	b, _ := json.Marshal(attrs)
	e.Attrs = b
	if _, _, err := s.DB.UpsertEntity(ctx, e); err != nil {
		slog.Warn("image update check", "entity", e.ID, "err", err)
		return
	}
	s.indexEntity(ctx, e)
	if d != digest && !was {
		_ = s.DB.AddProjectEvent(ctx, "system", "container.image_update", e.ID, e.ProjectID, map[string]any{"image": image, "latest": d})
	}
}
