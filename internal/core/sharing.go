package core

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/blob"
	"github.com/phoedox/phoedox/internal/store"
)

// DocSharing is who may see a document beyond the project roles.
type DocSharing struct {
	Restricted bool             `json:"restricted"`
	Grants     []store.DocGrant `json:"grants"`
}

// docOwner checks that the actor owns the project of a document (sharing
// and restricting are owner decisions).
func (s *Service) docOwner(ctx context.Context, a Actor, ref, docRef string) (store.Project, store.Document, error) {
	if err := s.fullUser(a); err != nil {
		return store.Project{}, store.Document{}, err
	}
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return p, store.Document{}, err
	}
	doc, err := s.findDoc(ctx, p.ID, docRef)
	return p, doc, err
}

// Sharing returns restriction and grants of a document (owners).
func (s *Service) Sharing(ctx context.Context, a Actor, ref, docRef string) (DocSharing, error) {
	_, doc, err := s.docOwner(ctx, a, ref, docRef)
	if err != nil {
		return DocSharing{}, err
	}
	gs, err := s.DB.DocGrants(ctx, doc.ID)
	return DocSharing{Restricted: doc.Restricted, Grants: gs}, err
}

// SetRestricted limits a document to owners and people it is shared with (owners).
func (s *Service) SetRestricted(ctx context.Context, a Actor, ref, docRef string, on bool) (DocSharing, error) {
	_, doc, err := s.docOwner(ctx, a, ref, docRef)
	if err != nil {
		return DocSharing{}, err
	}
	if err := s.DB.SetRestricted(ctx, doc.ID, on); err != nil {
		return DocSharing{}, err
	}
	s.event(ctx, a, "document.restrict", doc.ID, map[string]bool{"on": on})
	return s.Sharing(ctx, a, ref, docRef)
}

// GrantInput shares a document with a person (email) or a group.
type GrantInput struct {
	Email string `json:"email,omitempty"`
	Group string `json:"group,omitempty"`
	Role  string `json:"role"` // reader | editor
}

// ShareDocument shares a document with a person or a group of the project's
// organization; people outside the project then see only this document.
func (s *Service) ShareDocument(ctx context.Context, a Actor, ref, docRef string, in GrantInput) (DocSharing, error) {
	p, doc, err := s.docOwner(ctx, a, ref, docRef)
	if err != nil {
		return DocSharing{}, err
	}
	if in.Role != store.RoleReader && in.Role != store.RoleEditor {
		return DocSharing{}, invalid("validation.role", nil, "role must be reader or editor")
	}
	var userID, groupID string
	switch {
	case in.Group != "":
		g, err := s.DB.GetGroup(ctx, in.Group)
		if err != nil || g.OrgID == "" || g.OrgID != p.OrgID {
			return DocSharing{}, invalid("validation.group_org", nil, "only groups of the project's organization")
		}
		groupID = g.ID
	default:
		u, err := s.DB.UserByEmail(ctx, strings.TrimSpace(in.Email))
		if err != nil {
			return DocSharing{}, invalid("validation.user_not_found", map[string]any{"email": in.Email}, "no user with email %s", in.Email)
		}
		userID = u.ID
	}
	if err := s.DB.SetDocGrant(ctx, newID(), doc.ID, userID, groupID, in.Role); err != nil {
		return DocSharing{}, err
	}
	s.event(ctx, a, "document.share", doc.ID, map[string]string{"user": userID, "group": groupID, "role": in.Role})
	return s.Sharing(ctx, a, ref, docRef)
}

// UnshareDocument removes a grant (owners).
func (s *Service) UnshareDocument(ctx context.Context, a Actor, ref, docRef, grantID string) (DocSharing, error) {
	_, doc, err := s.docOwner(ctx, a, ref, docRef)
	if err != nil {
		return DocSharing{}, err
	}
	if err := s.DB.DeleteDocGrant(ctx, doc.ID, grantID); err != nil {
		return DocSharing{}, err
	}
	s.event(ctx, a, "document.unshare", doc.ID, map[string]string{"grant": grantID})
	return s.Sharing(ctx, a, ref, docRef)
}

// ShareLinkInput creates a read-only link to a project or one document.
type ShareLinkInput struct {
	Document string `json:"document,omitempty"` // slug or id; empty = whole project
	Label    string `json:"label,omitempty"`
	Days     int    `json:"days,omitempty"` // 0 = no expiry
}

// NewShareLink is a link together with its token (shown once).
type NewShareLink struct {
	store.ShareLink
	Token string `json:"token"`
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// CreateShareLink makes a read-only link: for the whole project (owners)
// or for one document (its editors; restricted documents only owners).
func (s *Service) CreateShareLink(ctx context.Context, a Actor, ref string, in ShareLinkInput) (NewShareLink, error) {
	if err := s.fullUser(a); err != nil {
		return NewShareLink{}, err
	}
	var p store.Project
	var docID string
	var err error
	if in.Document == "" {
		if p, err = s.project(ctx, a, ref, store.RoleOwner); err != nil {
			return NewShareLink{}, err
		}
	} else {
		var doc store.Document
		if p, doc, err = s.docAccess(ctx, a, ref, in.Document, store.RoleEditor); err != nil {
			return NewShareLink{}, err
		}
		if doc.Restricted && p.Role != store.RoleOwner {
			return NewShareLink{}, ErrForbidden
		}
		docID = doc.ID
	}
	if in.Days < 0 || in.Days > 3650 || len(in.Label) > 100 {
		return NewShareLink{}, invalid("validation.share_days", nil, "days must be between 0 and 3650")
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return NewShareLink{}, err
	}
	token := "phxs_" + base64.RawURLEncoding.EncodeToString(b)
	l := store.ShareLink{ID: newID(), ProjectID: p.ID, DocumentID: docID, Label: strings.TrimSpace(in.Label), CreatedBy: a.User.ID}
	if in.Days > 0 {
		l.ExpiresAt = store.Time{Time: time.Now().UTC().Add(time.Duration(in.Days) * 24 * time.Hour)}
	}
	l, err = s.DB.CreateShareLink(ctx, l, hashToken(token))
	if err != nil {
		return NewShareLink{}, err
	}
	s.event(ctx, a, "share.create", l.ID, map[string]any{"document": docID, "days": in.Days})
	return NewShareLink{ShareLink: l, Token: token}, nil
}

// ShareLinks lists the active links of a project (editors).
func (s *Service) ShareLinks(ctx context.Context, a Actor, ref string) ([]store.ShareLink, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	return s.DB.ShareLinks(ctx, p.ID)
}

// RevokeShareLink ends a link (owners, or the editor who made it).
func (s *Service) RevokeShareLink(ctx context.Context, a Actor, ref, id string) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	if p.Role != store.RoleOwner {
		ls, err := s.DB.ShareLinks(ctx, p.ID)
		if err != nil {
			return err
		}
		mine := false
		for _, l := range ls {
			mine = mine || (l.ID == id && l.CreatedBy == a.User.ID)
		}
		if !mine {
			return ErrForbidden
		}
	}
	if err := s.DB.RevokeShareLink(ctx, p.ID, id); err != nil {
		return err
	}
	s.event(ctx, a, "share.revoke", id, nil)
	return nil
}

// SharedView is what a share link shows: project name and color and the
// readable documents (without content).
type SharedView struct {
	Project struct {
		Name         string `json:"name"`
		PrimaryColor string `json:"primaryColor"`
	} `json:"project"`
	Single    bool             `json:"single"` // the link is for one document
	Documents []store.Document `json:"documents"`
}

func (s *Service) shareLink(ctx context.Context, token string) (store.ShareLink, store.Project, error) {
	if !strings.HasPrefix(token, "phxs_") {
		return store.ShareLink{}, store.Project{}, ErrNotFound
	}
	l, err := s.DB.ShareLinkByHash(ctx, hashToken(token))
	if err != nil {
		return l, store.Project{}, ErrNotFound
	}
	p, err := s.DB.ProjectFor(ctx, l.ProjectID, "")
	return l, p, err
}

// sharedDocs are the documents a link opens: its document, or all
// unrestricted documents of the project.
func (s *Service) sharedDocs(ctx context.Context, l store.ShareLink) ([]store.Document, error) {
	if l.DocumentID != "" {
		d, err := s.DB.GetDocument(ctx, l.ProjectID, l.DocumentID)
		if err != nil {
			return nil, err
		}
		return []store.Document{d}, nil
	}
	ds, err := s.DB.ListDocuments(ctx, l.ProjectID, store.DocFilter{})
	if err != nil {
		return nil, err
	}
	out := ds[:0]
	for _, d := range ds {
		if !d.Restricted {
			out = append(out, d)
		}
	}
	return out, nil
}

// Shared opens a share link (no account needed).
func (s *Service) Shared(ctx context.Context, token string) (SharedView, error) {
	l, p, err := s.shareLink(ctx, token)
	if err != nil {
		return SharedView{}, err
	}
	docs, err := s.sharedDocs(ctx, l)
	if err != nil {
		return SharedView{}, err
	}
	var v SharedView
	v.Project.Name, v.Project.PrimaryColor = p.Name, p.PrimaryColor
	v.Single = l.DocumentID != ""
	for i := range docs {
		docs[i].Content = nil
		docs[i].CreatedBy = ""
	}
	v.Documents = docs
	return v, nil
}

// SharedDocument returns one document of a share link with content.
func (s *Service) SharedDocument(ctx context.Context, token, docRef string) (store.Document, error) {
	l, _, err := s.shareLink(ctx, token)
	if err != nil {
		return store.Document{}, err
	}
	docs, err := s.sharedDocs(ctx, l)
	if err != nil {
		return store.Document{}, err
	}
	for _, d := range docs {
		if d.ID == docRef || d.Slug == docRef {
			full, err := s.DB.GetDocument(ctx, l.ProjectID, d.ID)
			full.CreatedBy = ""
			return full, err
		}
	}
	return store.Document{}, ErrNotFound
}

// SharedFile serves a file attached to a document of the share link.
func (s *Service) SharedFile(ctx context.Context, token, id string, preview bool) (FileContent, error) {
	l, _, err := s.shareLink(ctx, token)
	if err != nil {
		return FileContent{}, err
	}
	f, err := s.DB.GetFile(ctx, id)
	if err != nil || f.ProjectID != l.ProjectID || f.DocumentID == "" {
		return FileContent{}, ErrNotFound
	}
	docs, err := s.sharedDocs(ctx, l)
	if err != nil {
		return FileContent{}, err
	}
	ok := false
	for _, d := range docs {
		ok = ok || d.ID == f.DocumentID
	}
	if !ok || s.Blobs == nil {
		return FileContent{}, ErrNotFound
	}
	out := FileContent{File: f, Mime: f.Mime, SHA256: f.SHA256}
	if preview {
		if f.ThumbSHA256 == "" {
			return out, ErrNotFound
		}
		out.SHA256, out.Mime = f.ThumbSHA256, ""
	}
	rc, err := s.Blobs.Open(ctx, out.SHA256)
	if errors.Is(err, blob.ErrNotFound) {
		return out, ErrNotFound
	}
	out.Reader = rc
	return out, err
}
