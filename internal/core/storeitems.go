package core

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/store"
)

// Themes and app templates from the store (ui-themes-concept "Store",
// apps-definition "Templates"). They are content: a theme becomes a theme
// of a person, project, organization or the instance; an app template
// becomes an app document in a project. Both remember the store item and
// version they came from, so Phoedox can offer updates and rollbacks.
//
// Organizations decide per item type who may install from the store
// (everyone, chosen groups or nobody). People without access send a request
// to the organization's admins, unless the admins switched requests off.

// StoreRule says who in an organization may install one item type.
type StoreRule struct {
	Who    string   `json:"who"` // all (default) | groups | none
	Groups []string `json:"groups,omitempty"`
}

// StoreAccess is an organization's store setting.
type StoreAccess struct {
	Themes StoreRule `json:"themes"`
	Apps   StoreRule `json:"apps"`
	// NoRequests switches requests from people without access off.
	NoRequests bool `json:"noRequests,omitempty"`
}

func storeAccessKey(orgID string) string { return "store.access:" + orgID }

func (r StoreRule) valid() bool {
	switch r.Who {
	case "", "all", "none":
		return true
	case "groups":
		return len(r.Groups) <= 50
	}
	return false
}

func (s *Service) storeAccessOf(ctx context.Context, orgID string) StoreAccess {
	var acc StoreAccess
	if raw, _ := s.DB.Setting(ctx, storeAccessKey(orgID), ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &acc)
	}
	return acc
}

// OrgStoreAccess reads an organization's store access (its admins).
func (s *Service) OrgStoreAccess(ctx context.Context, a Actor, ref string) (StoreAccess, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return StoreAccess{}, err
	}
	return s.storeAccessOf(ctx, o.ID), nil
}

// SetOrgStoreAccess changes an organization's store access (its admins).
func (s *Service) SetOrgStoreAccess(ctx context.Context, a Actor, ref string, in StoreAccess) (StoreAccess, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return StoreAccess{}, err
	}
	if !in.Themes.valid() || !in.Apps.valid() {
		return StoreAccess{}, invalid("validation.store_access", nil, "who must be all, groups or none")
	}
	groups, _ := s.DB.Groups(ctx, o.ID)
	for _, r := range []*StoreRule{&in.Themes, &in.Apps} {
		if r.Who != "groups" {
			r.Groups = nil
		}
		r.Groups = slices.DeleteFunc(r.Groups, func(id string) bool {
			return !slices.ContainsFunc(groups, func(g store.Group) bool { return g.ID == id })
		})
	}
	raw, _ := json.Marshal(in)
	if err := s.DB.SetSetting(ctx, storeAccessKey(o.ID), string(raw), a.User.ID); err != nil {
		return StoreAccess{}, err
	}
	s.event(ctx, a, "org.store_access", o.ID, in)
	return in, nil
}

// storeAccess tells whether the person may install an item type into a
// project: yes, request (ask the organization's admins) or no. Personal
// projects and organization admins are not limited.
func (s *Service) storeAccess(ctx context.Context, a Actor, p store.Project, itemType string) string {
	if p.OrgID == "" || a.User.IsAdmin {
		return "yes"
	}
	if o, err := s.DB.OrgFor(ctx, p.OrgID, a.User.ID); err == nil && o.Role == store.OrgAdmin {
		return "yes"
	}
	acc := s.storeAccessOf(ctx, p.OrgID)
	rule := acc.Themes
	if itemType == plugins.TypeApp {
		rule = acc.Apps
	}
	ok := false
	switch rule.Who {
	case "", "all":
		ok = true
	case "groups":
		groups, _ := s.DB.Groups(ctx, p.OrgID)
		for _, g := range groups {
			if slices.Contains(rule.Groups, g.ID) && slices.ContainsFunc(g.Members, func(m store.OrgMember) bool { return m.UserID == a.User.ID }) {
				ok = true
			}
		}
	}
	switch {
	case ok:
		return "yes"
	case acc.NoRequests:
		return "no"
	}
	return "request"
}

// StoreItem is a theme or app template in the store with what is
// installed where the person looks.
type StoreItem struct {
	CatalogEntry
	// InstalledIn lists the person's themes (or the project's apps) made
	// from this item, with their version.
	InstalledIn []StoreInstall `json:"installedIn"`
}

// StoreInstall is a theme or app made from a store item.
type StoreInstall struct {
	ID      string `json:"id"`   // theme id or document id
	Name    string `json:"name"` // theme name or app title
	Scope   string `json:"scope,omitempty"`
	Owner   string `json:"owner,omitempty"` // project or organization slug
	Version string `json:"version"`
	// Update is the newer version the store offers ("" = up to date).
	Update  string `json:"update,omitempty"`
	CanEdit bool   `json:"canEdit"`
}

// StoreItems is the store's list of one item type.
type StoreItems struct {
	Items []StoreItem `json:"items"`
	// Access in the project asked for: yes | request | no.
	Access string `json:"access"`
	// Off: no store is configured on this instance.
	Off bool `json:"off,omitempty"`
}

func (s *Service) storeOn() bool { return s.Plugins.Store != nil }

func validItemType(t string) error {
	if t != plugins.TypeTheme && t != plugins.TypeApp {
		return invalid("validation.store_item_type", nil, "type must be theme or app")
	}
	return nil
}

// StoreCatalog lists the store's themes or app templates with what is
// installed: for themes all themes the person can see, for apps the apps of
// the project.
func (s *Service) StoreCatalog(ctx context.Context, a Actor, itemType, projectRef string) (StoreItems, error) {
	out := StoreItems{Items: []StoreItem{}, Access: "yes"}
	if err := s.fullUser(a); err != nil {
		return out, err
	}
	if err := validItemType(itemType); err != nil {
		return out, err
	}
	var p store.Project
	if projectRef != "" {
		var err error
		if p, err = s.project(ctx, a, projectRef, store.RoleReader); err != nil {
			return out, err
		}
		out.Access = s.storeAccess(ctx, a, p, itemType)
	} else if itemType == plugins.TypeApp {
		return out, invalid("validation.project_required", nil, "app templates need a project")
	}
	if !s.storeOn() {
		out.Off = true
		return out, nil
	}
	list, err := s.Plugins.Store.Items(ctx, s.Plugins.Version, itemType, s.pluginChannel(ctx))
	if err != nil {
		return out, invalid("validation.store_failed", map[string]any{"reason": err.Error()}, "store: %v", err)
	}
	installed := map[string][]StoreInstall{}
	if itemType == plugins.TypeTheme {
		cat, _ := s.Themes(ctx, a, projectRef)
		for _, t := range cat.Available {
			if t.OriginID != "" {
				installed[t.OriginID] = append(installed[t.OriginID], StoreInstall{ID: t.ID, Name: t.Name, Scope: t.Scope, Owner: t.Owner, Version: t.OriginVersion, CanEdit: t.CanEdit})
			}
		}
	} else {
		docs, _ := s.DB.ListDocuments(ctx, p.ID, store.DocFilter{Type: store.DocApp, WithContent: true})
		for _, d := range docs {
			if app := appOf(d); app.Origin != nil {
				installed[app.Origin.ID] = append(installed[app.Origin.ID], StoreInstall{ID: d.ID, Name: d.Title, Owner: p.Slug, Version: app.Origin.Version, CanEdit: p.Role != store.RoleReader})
			}
		}
	}
	for _, e := range list {
		item := StoreItem{CatalogEntry: e, InstalledIn: installed[e.ID]}
		for i, in := range item.InstalledIn {
			if plugins.CompareVersions(e.Version, in.Version) > 0 {
				item.InstalledIn[i].Update = e.Version
			}
		}
		if item.InstalledIn == nil {
			item.InstalledIn = []StoreInstall{}
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

// StoreVersions lists the versions of a store item that work with this
// Phoedox version, newest first (for rollbacks).
func (s *Service) StoreVersions(ctx context.Context, a Actor, id string) ([]CatalogEntry, error) {
	if err := s.fullUser(a); err != nil {
		return nil, err
	}
	if !s.storeOn() {
		return nil, invalid("validation.store_off", nil, "no plugin store configured")
	}
	list, err := s.Plugins.Store.Versions(ctx, id, s.Plugins.Version)
	if err != nil {
		return nil, invalid("validation.store_failed", map[string]any{"reason": err.Error()}, "store: %v", err)
	}
	return list, nil
}

// storePackage downloads a store item, checks the store's signature and
// that it is the expected type and fits this Phoedox version.
func (s *Service) storePackage(ctx context.Context, id, version, itemType string) (*plugins.Package, error) {
	if !s.storeOn() {
		return nil, invalid("validation.store_off", nil, "no plugin store configured")
	}
	data, sig, err := s.Plugins.Store.Download(ctx, id, version)
	if err != nil {
		return nil, invalid("validation.store_failed", map[string]any{"reason": err.Error()}, "store: %v", err)
	}
	p, err := plugins.ReadPackage(data)
	if err != nil {
		return nil, invalid("validation.plugin_package", map[string]any{"reason": strings.TrimPrefix(err.Error(), plugins.ErrPackage.Error()+": ")}, "%v", err)
	}
	if sig.Sig == "" || plugins.Verify(s.Plugins.Keys, p.Manifest.ID, p.Manifest.Version, p.SHA256, sig) != nil {
		return nil, invalid("validation.plugin_signature", nil, "the package signature is not valid")
	}
	if p.Manifest.ID != id || p.Manifest.Version != version || p.Manifest.Kind() != itemType {
		return nil, invalid("validation.store_item_mismatch", nil, "the store sent a different item")
	}
	if err := p.Manifest.Supported(s.Plugins.Version); err != nil {
		return nil, invalid("validation.plugin_incompatible", map[string]any{"reason": err.Error()}, "%v", err)
	}
	return p, nil
}

// StoreThemeInput installs (or updates, or rolls back) a store theme.
type StoreThemeInput struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	// Scope and Owner as for ThemeInput; an existing theme from the same
	// item and owner is replaced (people keep their choice).
	Scope string `json:"scope"`
	Owner string `json:"owner,omitempty"`
	// Theme: replace this theme (update or rollback) instead.
	Theme string `json:"theme,omitempty"`
}

// storeTheme reads theme.json and theme.css of a package.
func storeTheme(p *plugins.Package) (ThemeData, string, error) {
	var d ThemeData
	if err := json.Unmarshal(p.Files["theme.json"], &d); err != nil {
		return d, "", invalid("validation.plugin_package", map[string]any{"reason": "theme.json: " + err.Error()}, "theme.json: %v", err)
	}
	// Store themes bring no files: only built-in fonts, no logo.
	for _, f := range []*ThemeFont{&d.Body, &d.Display, &d.Mono} {
		f.URL, f.License = "", ""
		if f.Family != "" && !slices.Contains(ThemeFonts, f.Family) {
			f.Family = ""
		}
	}
	d.Logo, d.CSSOff = "", false
	css := string(p.Files["theme.css"])
	if err := validTheme(&d, css); err != nil {
		return d, "", err
	}
	return d, css, nil
}

// InstallStoreTheme makes a theme from a store item, or updates or rolls
// back a theme made from it.
func (s *Service) InstallStoreTheme(ctx context.Context, a Actor, in StoreThemeInput) (ThemeView, error) {
	if err := s.fullUser(a); err != nil {
		return ThemeView{}, err
	}
	var t store.Theme
	var ownerName string
	if in.Theme != "" {
		var err error
		if t, ownerName, err = s.themeAccess(ctx, a, in.Theme, true); err != nil {
			return ThemeView{}, err
		}
		if t.OriginID != in.ID {
			return ThemeView{}, invalid("validation.store_item_mismatch", nil, "the theme was not made from this item")
		}
	} else {
		owner, name, err := s.themeOwner(ctx, a, in.Scope, in.Owner, true)
		if err != nil {
			return ThemeView{}, err
		}
		if in.Scope == "project" {
			p, _ := s.project(ctx, a, in.Owner, store.RoleOwner)
			if acc := s.storeAccess(ctx, a, p, plugins.TypeTheme); acc != "yes" {
				return ThemeView{}, errStoreAccess(acc)
			}
		}
		t = store.Theme{ID: newID(), Scope: in.Scope, OwnerID: owner, CreatedBy: a.User.ID, CreatedAt: store.Now()}
		ownerName = name
		if ts, err := s.DB.Themes(ctx, in.Scope, owner); err == nil {
			for _, old := range ts {
				if old.OriginID == in.ID {
					t = old
				}
			}
		}
	}
	p, err := s.storePackage(ctx, in.ID, in.Version, plugins.TypeTheme)
	if err != nil {
		return ThemeView{}, err
	}
	data, css, err := storeTheme(p)
	if err != nil {
		return ThemeView{}, err
	}
	raw, _ := json.Marshal(data)
	t.Name, t.Data, t.CSS, t.UpdatedAt = excerpt(p.Manifest.Name, 80), raw, css, store.Now()
	t.OriginID, t.OriginVersion = p.Manifest.ID, p.Manifest.Version
	// The store checked the CSS and the version range matches this Phoedox.
	t.CSSReleased = ""
	if css != "" {
		t.CSSReleased = s.appVersion()
	}
	if err := s.DB.SaveTheme(ctx, t); err != nil {
		return ThemeView{}, err
	}
	s.event(ctx, a, "theme.store_install", t.ID, map[string]string{"item": t.OriginID, "version": t.OriginVersion, "scope": t.Scope})
	return s.themeView(a, t, ownerName, true), nil
}

// AppOrigin is the store item an app was made from.
type AppOrigin struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Name    string `json:"name,omitempty"`
}

// storeApp reads app.json and files/ of a package.
func storeApp(p *plugins.Package) (*App, error) {
	app := &App{Files: map[string]string{}}
	if err := json.Unmarshal(p.Files["app.json"], app); err != nil {
		return nil, invalid("validation.plugin_package", map[string]any{"reason": "app.json: " + err.Error()}, "app.json: %v", err)
	}
	app.Files, app.Build = map[string]string{}, nil
	for name, b := range p.Files {
		if base, ok := strings.CutPrefix(name, "files/"); ok && path.Base(base) == base {
			app.Files[base] = string(b)
		}
	}
	app.Origin = &AppOrigin{ID: p.Manifest.ID, Version: p.Manifest.Version, Name: p.Manifest.Name}
	return app, nil
}

// StoreAppInput makes an app from a store template, or updates an app made
// from one.
type StoreAppInput struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Title    string `json:"title,omitempty"`
	Category string `json:"category,omitempty"`
	// Document: update this app (its storage stays) instead.
	Document string `json:"document,omitempty"`
}

func errStoreAccess(acc string) error {
	if acc == "request" {
		return invalid("validation.store_request_needed", nil, "your organization asks you to request this item")
	}
	return ErrForbidden
}

// InstallStoreApp creates an app from a store template in a project, or
// updates (or rolls back) an app made from one.
func (s *Service) InstallStoreApp(ctx context.Context, a Actor, ref string, in StoreAppInput) (DocResult, error) {
	pr, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return DocResult{}, err
	}
	if acc := s.storeAccess(ctx, a, pr, plugins.TypeApp); acc != "yes" {
		return DocResult{}, errStoreAccess(acc)
	}
	if in.Document != "" {
		_, doc, err := s.docAccess(ctx, a, ref, in.Document, store.RoleEditor)
		if err != nil {
			return DocResult{}, err
		}
		cur := appOf(doc)
		if doc.Type != store.DocApp || cur.Origin == nil || cur.Origin.ID != in.ID {
			return DocResult{}, invalid("validation.store_item_mismatch", nil, "the app was not made from this item")
		}
		p, err := s.storePackage(ctx, in.ID, in.Version, plugins.TypeApp)
		if err != nil {
			return DocResult{}, err
		}
		app, err := storeApp(p)
		if err != nil {
			return DocResult{}, err
		}
		res, err := s.UpdateDocument(ctx, a, ref, doc.ID, DocumentPatch{App: app})
		if err == nil {
			s.event(ctx, a, "app.store_update", doc.ID, map[string]string{"item": in.ID, "version": in.Version})
		}
		return res, err
	}
	p, err := s.storePackage(ctx, in.ID, in.Version, plugins.TypeApp)
	if err != nil {
		return DocResult{}, err
	}
	app, err := storeApp(p)
	if err != nil {
		return DocResult{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = p.Manifest.Name
	}
	res, err := s.CreateDocument(ctx, a, ref, DocumentInput{Type: store.DocApp, Title: title, Category: in.Category, App: app})
	// A second copy of the same template gets a number.
	for n := 2; errors.Is(err, store.ErrConflict) && n <= 20; n++ {
		res, err = s.CreateDocument(ctx, a, ref, DocumentInput{Type: store.DocApp, Title: title + " " + strconv.Itoa(n), Category: in.Category, App: app})
	}
	if err == nil {
		s.event(ctx, a, "app.store_install", res.Document.ID, map[string]string{"item": in.ID, "version": in.Version})
	}
	return res, err
}

// --- requests --------------------------------------------------------------

// StoreRequestInput asks for a store item in a project.
type StoreRequestInput struct {
	Type    string `json:"type"` // theme | app
	ID      string `json:"id"`
	Version string `json:"version"`
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Note    string `json:"note,omitempty"`
}

// RequestStoreItem asks the organization's admins to install a theme or app
// template into a project.
func (s *Service) RequestStoreItem(ctx context.Context, a Actor, ref string, in StoreRequestInput) (store.StoreRequest, error) {
	if err := s.fullUser(a); err != nil {
		return store.StoreRequest{}, err
	}
	if err := validItemType(in.Type); err != nil {
		return store.StoreRequest{}, err
	}
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return store.StoreRequest{}, err
	}
	switch s.storeAccess(ctx, a, p, in.Type) {
	case "yes":
		return store.StoreRequest{}, invalid("validation.store_no_request", nil, "you can install it yourself")
	case "no":
		return store.StoreRequest{}, invalid("validation.requests_off", nil, "the organization does not take requests")
	}
	if in.ID == "" || in.Version == "" {
		return store.StoreRequest{}, invalid("validation.store_item_mismatch", nil, "id and version are required")
	}
	open, _ := s.DB.StoreRequests(ctx, p.OrgID, a.User.ID, "pending")
	if slices.ContainsFunc(open, func(r store.StoreRequest) bool { return r.ItemID == in.ID && r.ProjectID == p.ID }) {
		return store.StoreRequest{}, invalid("validation.request_pending", nil, "a request is already waiting for an answer")
	}
	r := store.StoreRequest{ID: newID(), OrgID: p.OrgID, ProjectID: p.ID, ProjectSlug: p.Slug, ProjectName: p.Name, ItemType: in.Type, ItemID: in.ID,
		ItemName: excerpt(strings.TrimSpace(in.Name), 120), Version: in.Version, Title: excerpt(strings.TrimSpace(in.Title), 200),
		Note: excerpt(strings.TrimSpace(in.Note), 1000), Status: "pending", RequestedBy: a.User.ID, UserName: a.User.Name, CreatedAt: store.Now()}
	if r.ItemName == "" {
		r.ItemName = in.ID
	}
	if err := s.DB.AddStoreRequest(ctx, r); err != nil {
		return r, err
	}
	s.event(ctx, a, "org.store_request", p.OrgID, r)
	o, _ := s.DB.OrgFor(ctx, p.OrgID, "")
	ms, _ := s.DB.OrgMembers(ctx, p.OrgID)
	for _, m := range ms {
		if m.Role != store.OrgAdmin {
			continue
		}
		if u, err := s.DB.UserByID(ctx, m.UserID); err == nil {
			s.notify(ctx, u, Note{Kind: "store.request", Subject: r.ID, ProjectID: p.ID, Project: p.Name, Title: r.ItemName, Body: r.Note, Actor: a.User.Name, Link: "/orgs/" + o.Slug + "#store"})
		}
	}
	return r, nil
}

// StoreRequests lists an organization's requests (its admins).
func (s *Service) StoreRequests(ctx context.Context, a Actor, ref string) ([]store.StoreRequest, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return nil, err
	}
	return s.DB.StoreRequests(ctx, o.ID, "", "")
}

// MyStoreRequests lists the person's requests in the organization of a
// project.
func (s *Service) MyStoreRequests(ctx context.Context, a Actor, ref string) ([]store.StoreRequest, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	if p.OrgID == "" {
		return []store.StoreRequest{}, nil
	}
	all, err := s.DB.StoreRequests(ctx, p.OrgID, a.User.ID, "")
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(all, func(r store.StoreRequest) bool { return r.ProjectID != p.ID }), nil
}

// StoreDecision answers a request.
type StoreDecision struct {
	Approve bool   `json:"approve"`
	Answer  string `json:"answer,omitempty"`
}

// DecideStoreRequest approves (and installs) or rejects a request
// (organization admins). Approving installs the item into the project in
// the admin's name: an app template becomes an app, a theme a project
// theme.
func (s *Service) DecideStoreRequest(ctx context.Context, a Actor, ref, id string, in StoreDecision) (store.StoreRequest, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return store.StoreRequest{}, err
	}
	r, err := s.DB.GetStoreRequest(ctx, o.ID, id)
	if err != nil {
		return r, err
	}
	if r.Status != "pending" {
		return r, invalid("validation.request_decided", nil, "the request was already answered")
	}
	now := store.Now()
	r.Status, r.Answer, r.DecidedBy, r.DecidedAt = "rejected", excerpt(strings.TrimSpace(in.Answer), 1000), a.User.ID, &now
	if in.Approve {
		r.Status = "approved"
		switch r.ItemType {
		case plugins.TypeApp:
			res, err := s.InstallStoreApp(ctx, a, r.ProjectID, StoreAppInput{ID: r.ItemID, Version: r.Version, Title: r.Title})
			if err != nil {
				return r, err
			}
			r.Result = res.Document.Slug
		default:
			v, err := s.InstallStoreTheme(ctx, a, StoreThemeInput{ID: r.ItemID, Version: r.Version, Scope: "project", Owner: r.ProjectID})
			if err != nil {
				return r, err
			}
			r.Result = v.ID
		}
	}
	if err := s.DB.DecideStoreRequest(ctx, r); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return r, invalid("validation.request_decided", nil, "the request was already answered")
		}
		return r, err
	}
	s.event(ctx, a, "org.store_decision", o.ID, r)
	if u, err := s.DB.UserByID(ctx, r.RequestedBy); err == nil {
		link := "/p/" + r.ProjectSlug
		if r.ItemType == plugins.TypeApp && r.Result != "" {
			link += "/d/" + r.Result
		}
		s.notify(ctx, u, Note{Kind: "store.request_decided", Subject: r.ID, ProjectID: r.ProjectID, Project: r.ProjectName, Title: r.ItemName, Body: r.Answer, Actor: a.User.Name, Link: link})
	}
	return r, nil
}
