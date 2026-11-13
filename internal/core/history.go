package core

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/store"
)

// VersionInfo is a version with the name of who made it.
type VersionInfo struct {
	store.Version
	ActorName string   `json:"actorName,omitempty"`
	ViaAI     bool     `json:"viaAI,omitempty"`
	Coauthors []string `json:"coauthors,omitempty"` // names
}

func (s *Service) withNames(ctx context.Context, vs []store.Version) ([]VersionInfo, error) {
	var ids []string
	for _, v := range vs {
		if id := store.ActorUserID(v.Actor); id != "" {
			ids = append(ids, id)
		}
		if v.Coauthors != "" {
			ids = append(ids, strings.Split(v.Coauthors, ",")...)
		}
	}
	names, err := s.DB.UserNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]VersionInfo, len(vs))
	for i, v := range vs {
		out[i] = VersionInfo{Version: v, ActorName: names[store.ActorUserID(v.Actor)], ViaAI: len(v.Actor) > 4 && v.Actor[:4] == "mcp:"}
		if v.Coauthors != "" {
			for _, id := range strings.Split(v.Coauthors, ",") {
				if n := names[id]; n != "" {
					out[i].Coauthors = append(out[i].Coauthors, n)
				}
			}
		}
	}
	return out, nil
}

// VersionHistory lists the versions of a document with author names.
func (s *Service) VersionHistory(ctx context.Context, a Actor, ref, docRef string) ([]VersionInfo, error) {
	vs, err := s.Versions(ctx, a, ref, docRef)
	if err != nil {
		return nil, err
	}
	return s.withNames(ctx, vs)
}

// DocumentVersion returns one version with its content.
func (s *Service) DocumentVersion(ctx context.Context, a Actor, ref, docRef string, version int) (VersionInfo, error) {
	doc, err := s.Document(ctx, a, ref, docRef)
	if err != nil {
		return VersionInfo{}, err
	}
	v, err := s.DB.GetVersion(ctx, doc.ID, version)
	if err != nil {
		return VersionInfo{}, err
	}
	out, err := s.withNames(ctx, []store.Version{v})
	if err != nil {
		return VersionInfo{}, err
	}
	return out[0], nil
}

// RestoreVersion saves an old version as the newest one. It goes through
// UpdateDocument, so suggestion mode, links and the index apply as usual.
func (s *Service) RestoreVersion(ctx context.Context, a Actor, ref, docRef string, version int) (DocResult, error) {
	doc, err := s.Document(ctx, a, ref, docRef)
	if err != nil {
		return DocResult{}, err
	}
	v, err := s.DB.GetVersion(ctx, doc.ID, version)
	if err != nil {
		return DocResult{}, err
	}
	patch := DocumentPatch{Title: &v.Title}
	switch doc.Type {
	case store.DocMarkdown:
		var c struct {
			Markdown string `json:"markdown"`
		}
		_ = json.Unmarshal(v.Content, &c)
		patch.Markdown = &c.Markdown
	case store.DocADR:
		var c ADR
		_ = json.Unmarshal(v.Content, &c)
		patch.ADR = &c
	case store.DocLink:
		var c Link
		_ = json.Unmarshal(v.Content, &c)
		patch.Link = &c
	}
	res, err := s.UpdateDocument(ctx, a, ref, doc.ID, patch)
	if err == nil && res.Applied {
		s.event(ctx, a, "document.restore_version", doc.ID, map[string]int{"from": version, "version": res.Document.Version})
	}
	return res, err
}

// Trash lists deleted documents and entities of a project.
func (s *Service) Trash(ctx context.Context, a Actor, ref string) ([]store.TrashItem, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	return s.DB.Trash(ctx, p.ID)
}

// RestoreFromTrash brings a document or entity back.
func (s *Service) RestoreFromTrash(ctx context.Context, a Actor, ref, id string) (store.TrashItem, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return store.TrashItem{}, err
	}
	if doc, err := s.DB.RestoreDocument(ctx, p.ID, id); err == nil {
		s.indexDocument(ctx, doc)
		s.event(ctx, a, "document.restore", doc.ID, nil)
		return store.TrashItem{ID: doc.ID, Kind: "document", Type: doc.Type, Slug: doc.Slug, Title: doc.Title}, nil
	} else if errors.Is(err, store.ErrConflict) {
		return store.TrashItem{}, invalid("validation.slug_taken", nil, "another document uses this slug now")
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.TrashItem{}, err
	}
	if f, err := s.DB.RestoreFile(ctx, p.ID, id); err == nil {
		s.event(ctx, a, "file.restore", f.ID, nil)
		return store.TrashItem{ID: f.ID, Kind: "file", Type: f.Mime, Slug: f.ID, Title: f.Name}, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return store.TrashItem{}, err
	}
	e, err := s.DB.RestoreEntity(ctx, p.ID, id)
	if errors.Is(err, store.ErrConflict) {
		return store.TrashItem{}, invalid("validation.slug_taken", nil, "another entry uses this slug now")
	}
	if err != nil {
		return store.TrashItem{}, err
	}
	s.indexEntity(ctx, e)
	s.event(ctx, a, "entity.restore", e.ID, nil)
	return store.TrashItem{ID: e.ID, Kind: "entity", Type: e.Kind, Slug: e.Slug, Title: e.Name}, nil
}

// PurgeFromTrash deletes a trashed item for good (owners only).
func (s *Service) PurgeFromTrash(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return err
	}
	hashes, err := s.DB.TrashedFileHashes(ctx, p.ID, id)
	if err != nil {
		return err
	}
	if err := s.DB.Purge(ctx, p.ID, id); err != nil {
		return err
	}
	s.freeBlobs(ctx, hashes)
	s.event(ctx, a, "trash.purge", id, nil)
	return nil
}

// PurgeExpiredTrash removes items trashed more than days ago (0 = never).
func (s *Service) PurgeExpiredTrash(ctx context.Context, days int) {
	if days <= 0 {
		return
	}
	before := store.Now()
	before.Time = before.Time.Add(-time.Duration(days) * 24 * time.Hour)
	hashes, err := s.DB.ExpiredFileHashes(ctx, before)
	if err != nil {
		slog.Warn("emptying the trash failed", "err", err)
		return
	}
	defer s.freeBlobs(ctx, hashes)
	if n, err := s.DB.PurgeBefore(ctx, before); err != nil {
		slog.Warn("emptying the trash failed", "err", err)
	} else if n > 0 {
		slog.Info("trash emptied", "items", n, "days", days)
		_ = s.DB.AddEvent(ctx, "system", "trash.expire", "", map[string]int{"items": n, "days": days})
	}
}

// Favorites returns project favorites and the actor's own ones.
func (s *Service) Favorites(ctx context.Context, a Actor, ref string) ([]store.Favorite, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	return s.DB.Favorites(ctx, p.ID, a.User.ID)
}

// SetFavorite adds or removes a favorite. Project favorites (personal
// false) need editor rights; personal ones only reading rights.
func (s *Service) SetFavorite(ctx context.Context, a Actor, ref, docRef string, personal, on bool) error {
	need := store.RoleEditor
	if personal {
		need = store.RoleReader
	}
	p, err := s.project(ctx, a, ref, need)
	if err != nil {
		return err
	}
	doc, err := s.findDoc(ctx, p.ID, docRef)
	if err != nil {
		return err
	}
	user := ""
	if personal {
		user = a.User.ID
	}
	if on {
		err = s.DB.AddFavorite(ctx, newID(), p.ID, doc.ID, user)
	} else {
		err = s.DB.RemoveFavorite(ctx, p.ID, doc.ID, user)
	}
	if err == nil && !personal {
		s.event(ctx, a, "favorite.project", doc.ID, map[string]bool{"on": on})
	}
	return err
}

// ReorderFavorites sets the order of the project's or the actor's favorites.
func (s *Service) ReorderFavorites(ctx context.Context, a Actor, ref string, personal bool, docIDs []string) error {
	need := store.RoleEditor
	if personal {
		need = store.RoleReader
	}
	p, err := s.project(ctx, a, ref, need)
	if err != nil {
		return err
	}
	user := ""
	if personal {
		user = a.User.ID
	}
	return s.DB.ReorderFavorites(ctx, p.ID, user, docIDs)
}
