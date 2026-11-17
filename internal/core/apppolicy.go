package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// App policy (apps-definition "Admin switches"): the instance and each
// organization can switch apps off, cap the bridge level, forbid AI calls
// and allow network hosts apps may connect to. The stricter setting wins;
// hosts of an organization count only where the instance lets
// organizations add hosts.

// AppPolicy is one level's switches.
type AppPolicy struct {
	Off bool `json:"off,omitempty"`
	// MaxLevel caps the bridge level (0–3); nil = no cap.
	MaxLevel *int `json:"maxLevel,omitempty"`
	NoAI     bool `json:"noAi,omitempty"`
	// NoPublic forbids releasing apps to people without an account.
	NoPublic bool `json:"noPublic,omitempty"`
	// NoWebEmbeds switches interactive website embeds on whiteboards off
	// (they stay link cards); EmbedSites limits them to these sites and
	// their subdomains (empty = any site that allows framing).
	NoWebEmbeds bool     `json:"noWebEmbeds,omitempty"`
	EmbedSites  []string `json:"embedSites,omitempty"`
	// Hosts apps may connect to (https only, exact host names).
	Hosts []string `json:"hosts,omitempty"`
	// OrgHosts (instance only): organizations may allow their own hosts.
	OrgHosts bool `json:"orgHosts,omitempty"`
}

const settingAppPolicy = "apps.policy"

func appOrgKey(orgID string) string { return "apps.org:" + orgID }

var appHostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

func (p *AppPolicy) normalize() error {
	if p.MaxLevel != nil && (*p.MaxLevel < 0 || *p.MaxLevel > 3) {
		return invalid("validation.app_level", nil, "the level is 0 to 3")
	}
	hosts := []string{}
	for _, h := range p.Hosts {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "https://")))
		if h == "" {
			continue
		}
		if !appHostRe.MatchString(h) {
			return invalid("validation.app_host", map[string]any{"host": h}, "%q is not a host name", h)
		}
		if !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	if len(hosts) > 50 {
		return invalid("validation.app_host", map[string]any{"host": "…"}, "at most 50 hosts")
	}
	p.Hosts = hosts
	sites := []string{}
	for _, h := range p.EmbedSites {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(h, "https://"), "http://")))
		h = strings.TrimSuffix(h, "/")
		if h == "" {
			continue
		}
		if !appHostRe.MatchString(h) {
			return invalid("validation.app_host", map[string]any{"host": h}, "%q is not a host name", h)
		}
		if !slices.Contains(sites, h) {
			sites = append(sites, h)
		}
	}
	if len(sites) > 200 {
		return invalid("validation.app_host", map[string]any{"host": "…"}, "at most 200 sites")
	}
	p.EmbedSites = sites
	return nil
}

// siteIn reports whether host is one of the sites or a subdomain of one.
func siteIn(host string, sites []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, s := range sites {
		if host == s || strings.HasSuffix(host, "."+s) {
			return true
		}
	}
	return false
}

// EmbedPolicy is what whiteboards of a project may embed live.
type EmbedPolicy struct {
	Web   bool     `json:"web"`
	Sites []string `json:"sites"` // empty = any site that allows framing
}

// embedPolicy combines the instance's and the organization's embed rules:
// either can switch websites off; with two lists only sites on both count.
func (s *Service) embedPolicy(ctx context.Context, p store.Project) EmbedPolicy {
	inst := s.readAppPolicy(ctx, settingAppPolicy)
	out := EmbedPolicy{Web: !inst.NoWebEmbeds, Sites: slices.Clone(inst.EmbedSites)}
	if p.OrgID != "" {
		org := s.readAppPolicy(ctx, appOrgKey(p.OrgID))
		out.Web = out.Web && !org.NoWebEmbeds
		switch {
		case len(org.EmbedSites) == 0:
		case len(out.Sites) == 0:
			out.Sites = slices.Clone(org.EmbedSites)
		default:
			out.Sites = slices.DeleteFunc(slices.Clone(org.EmbedSites), func(h string) bool { return !siteIn(h, inst.EmbedSites) })
			if len(out.Sites) == 0 {
				out.Web = false
			}
		}
	}
	if out.Sites == nil {
		out.Sites = []string{}
	}
	return out
}

// allows reports whether a website may be embedded live.
func (e EmbedPolicy) allows(host string) bool {
	return e.Web && (len(e.Sites) == 0 || siteIn(host, e.Sites))
}

// ProjectEmbedPolicy tells the whiteboard editor which websites it may embed live.
func (s *Service) ProjectEmbedPolicy(ctx context.Context, a Actor, ref string) (EmbedPolicy, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return EmbedPolicy{}, err
	}
	return s.embedPolicy(ctx, p), nil
}

func (s *Service) readAppPolicy(ctx context.Context, key string) AppPolicy {
	var p AppPolicy
	if raw, _ := s.DB.Setting(ctx, key, ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &p)
	}
	return p
}

// effectiveAppPolicy combines the instance's and the project's
// organization's switches.
func (s *Service) effectiveAppPolicy(ctx context.Context, p store.Project) AppPolicy {
	inst := s.readAppPolicy(ctx, settingAppPolicy)
	out := AppPolicy{Off: inst.Off, MaxLevel: inst.MaxLevel, NoAI: inst.NoAI, NoPublic: inst.NoPublic, Hosts: slices.Clone(inst.Hosts)}
	if p.OrgID != "" {
		org := s.readAppPolicy(ctx, appOrgKey(p.OrgID))
		out.Off = out.Off || org.Off
		out.NoAI = out.NoAI || org.NoAI
		out.NoPublic = out.NoPublic || org.NoPublic
		if org.MaxLevel != nil && (out.MaxLevel == nil || *org.MaxLevel < *out.MaxLevel) {
			out.MaxLevel = org.MaxLevel
		}
		if inst.OrgHosts {
			for _, h := range org.Hosts {
				if !slices.Contains(out.Hosts, h) {
					out.Hosts = append(out.Hosts, h)
				}
			}
		}
	}
	return out
}

// capLevel applies the policy's level cap.
func (p AppPolicy) capLevel(level int) int {
	if p.MaxLevel != nil {
		return min(level, *p.MaxLevel)
	}
	return level
}

func errAppsOff() error { return invalid("validation.apps_off", nil, "apps are switched off here") }

// InstanceAppPolicy reads the instance's app switches (system admins).
func (s *Service) InstanceAppPolicy(ctx context.Context, a Actor) (AppPolicy, error) {
	if err := s.adminOnly(a); err != nil {
		return AppPolicy{}, err
	}
	return s.readAppPolicy(ctx, settingAppPolicy), nil
}

// SetInstanceAppPolicy changes the instance's app switches.
func (s *Service) SetInstanceAppPolicy(ctx context.Context, a Actor, in AppPolicy) (AppPolicy, error) {
	if err := s.adminOnly(a); err != nil {
		return AppPolicy{}, err
	}
	if err := in.normalize(); err != nil {
		return AppPolicy{}, err
	}
	raw, _ := json.Marshal(in)
	if err := s.DB.SetSetting(ctx, settingAppPolicy, string(raw), a.User.ID); err != nil {
		return AppPolicy{}, err
	}
	s.event(ctx, a, "apps.policy", "instance", in)
	return in, nil
}

// OrgAppPolicy reads an organization's app switches (its admins); Hosts
// counts only if the instance allows organization hosts (InstanceOrgHosts).
type OrgAppPolicy struct {
	AppPolicy
	InstanceOrgHosts bool `json:"instanceOrgHosts"`
}

// GetOrgAppPolicy reads an organization's app switches.
func (s *Service) GetOrgAppPolicy(ctx context.Context, a Actor, ref string) (OrgAppPolicy, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return OrgAppPolicy{}, err
	}
	return OrgAppPolicy{AppPolicy: s.readAppPolicy(ctx, appOrgKey(o.ID)), InstanceOrgHosts: s.readAppPolicy(ctx, settingAppPolicy).OrgHosts}, nil
}

// SetOrgAppPolicy changes an organization's app switches (its admins).
func (s *Service) SetOrgAppPolicy(ctx context.Context, a Actor, ref string, in AppPolicy) (OrgAppPolicy, error) {
	o, err := s.org(ctx, a, ref, store.OrgAdmin)
	if err != nil {
		return OrgAppPolicy{}, err
	}
	in.OrgHosts = false
	if err := in.normalize(); err != nil {
		return OrgAppPolicy{}, err
	}
	raw, _ := json.Marshal(in)
	if err := s.DB.SetSetting(ctx, appOrgKey(o.ID), string(raw), a.User.ID); err != nil {
		return OrgAppPolicy{}, err
	}
	s.event(ctx, a, "apps.policy", o.ID, in)
	return OrgAppPolicy{AppPolicy: in, InstanceOrgHosts: s.readAppPolicy(ctx, settingAppPolicy).OrgHosts}, nil
}

// --- app templates in a project ---------------------------------------------

func appTemplatesKey(projectID string) string { return "apps.templates:" + projectID }

func (s *Service) appTemplateIDs(ctx context.Context, projectID string) []string {
	var ids []string
	if raw, _ := s.DB.Setting(ctx, appTemplatesKey(projectID), ""); raw != "" {
		_ = json.Unmarshal([]byte(raw), &ids)
	}
	return ids
}

// AppTemplate is an app of the project offered as template.
type AppTemplate struct {
	Document string `json:"document"` // slug
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category,omitempty"`
}

// AppTemplates lists the project's app templates (readers).
func (s *Service) AppTemplates(ctx context.Context, a Actor, ref string) ([]AppTemplate, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	out := []AppTemplate{}
	for _, id := range s.appTemplateIDs(ctx, p.ID) {
		if d, err := s.DB.GetDocument(ctx, p.ID, id); err == nil && d.Type == store.DocApp {
			out = append(out, AppTemplate{Document: d.Slug, ID: d.ID, Title: d.Title, Category: d.Category})
		}
	}
	return out, nil
}

// SetAppTemplate marks an app as template of its project, or not (editors).
func (s *Service) SetAppTemplate(ctx context.Context, a Actor, ref, docRef string, on bool) ([]AppTemplate, error) {
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleEditor)
	if err != nil {
		return nil, err
	}
	if doc.Type != store.DocApp {
		return nil, invalid("validation.not_app", nil, "%s is not an app", docRef)
	}
	ids := slices.DeleteFunc(s.appTemplateIDs(ctx, p.ID), func(x string) bool { return x == doc.ID })
	if on {
		ids = append(ids, doc.ID)
	}
	raw, _ := json.Marshal(ids)
	if err := s.DB.SetSetting(ctx, appTemplatesKey(p.ID), string(raw), a.User.ID); err != nil {
		return nil, err
	}
	s.event(ctx, a, "app.template", doc.ID, map[string]bool{"on": on})
	return s.AppTemplates(ctx, a, ref)
}

// AppCopyInput makes a new app from an existing one.
type AppCopyInput struct {
	Title    string `json:"title"`
	Category string `json:"category,omitempty"`
}

// CopyApp creates a new app with the files and manifest of another (its
// storage and grants stay with the original).
func (s *Service) CopyApp(ctx context.Context, a Actor, ref, docRef string, in AppCopyInput) (DocResult, error) {
	_, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return DocResult{}, err
	}
	if doc.Type != store.DocApp {
		return DocResult{}, invalid("validation.not_app", nil, "%s is not an app", docRef)
	}
	app := appOf(doc)
	app.Build, app.Origin = nil, nil
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = doc.Title
	}
	res, err := s.CreateDocument(ctx, a, ref, DocumentInput{Type: store.DocApp, Title: title, Category: in.Category, App: &app})
	for n := 2; errors.Is(err, store.ErrConflict) && n <= 20; n++ {
		res, err = s.CreateDocument(ctx, a, ref, DocumentInput{Type: store.DocApp, Title: fmt.Sprintf("%s %d", title, n), Category: in.Category, App: &app})
	}
	if err == nil {
		s.event(ctx, a, "app.copy", res.Document.ID, map[string]string{"from": doc.ID})
	}
	return res, err
}
