package core

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/plugins"
	"github.com/phoedox/phoedox/internal/store"
)

// Devices (ADR-0050): entries a plugin's control tool acts on. Owners decide
// per device whether AI clients may switch it without a confirmation, and
// let single people switch it without any other rights in the project.
// Every switch is recorded on the device.

// DeviceTool is a control tool that acts on a device.
type DeviceTool struct {
	Plugin      string          `json:"plugin"`
	PluginName  string          `json:"pluginName"`
	Tool        string          `json:"tool"`
	Description string          `json:"description"`
	Input       json.RawMessage `json:"input,omitempty"` // JSON Schema of the arguments
	Arg         string          `json:"arg"`             // filled with Value by Phoedox
	Value       string          `json:"value"`
}

// DeviceInfo is what the device panel of an entry shows.
type DeviceInfo struct {
	Tools      []DeviceTool         `json:"tools"`
	AIControl  string               `json:"aiControl"`
	CanControl bool                 `json:"canControl"`
	CanManage  bool                 `json:"canManage"`
	Grants     []store.ControlGrant `json:"grants,omitempty"`
}

// SharedDevice is a device someone shared with the signed-in person.
type SharedDevice struct {
	Grant       string       `json:"grant"`
	Entity      string       `json:"entity"`
	Name        string       `json:"name"`
	Kind        string       `json:"kind"`
	ProjectName string       `json:"projectName"`
	SharedBy    string       `json:"sharedBy"`
	ExpiresAt   store.Time   `json:"expiresAt"`
	Tools       []DeviceTool `json:"tools"`
}

// DeviceGrantInput shares a device with a person for a number of days (0 = until revoked).
type DeviceGrantInput struct {
	Email string `json:"email"`
	Days  int    `json:"days"`
}

// controlTarget finds the entry a control call acts on.
func (s *Service) controlTarget(ctx context.Context, p store.Project, ap *activePlugin, def plugins.ToolDef, args json.RawMessage) (store.Entity, bool) {
	if def.Target == nil {
		return store.Entity{}, false
	}
	var m map[string]any
	if json.Unmarshal(args, &m) != nil {
		return store.Entity{}, false
	}
	v, _ := m[def.Target.Arg].(string)
	if v == "" {
		return store.Entity{}, false
	}
	if def.Target.Attr == "" {
		e, err := s.DB.GetEntity(ctx, p.ID, v)
		return e, err == nil && (ap == nil || ownKind(*ap, e.Kind))
	}
	ents, err := s.DB.ListEntities(ctx, p.ID, "")
	if err != nil {
		return store.Entity{}, false
	}
	for _, e := range ents {
		if attrString(e, def.Target.Attr) == v {
			return e, true
		}
	}
	return store.Entity{}, false
}

// ownKind tells whether an entry is of a kind the plugin contributes:
// targets without an attribute are slugs of such entries.
func ownKind(ap activePlugin, kind string) bool {
	return slices.ContainsFunc(ap.pkg.Manifest.Contributes.Kinds, func(k plugins.KindDef) bool { return k.ID == kind })
}

func attrString(e store.Entity, name string) string {
	var attrs map[string]any
	_ = json.Unmarshal(e.Attrs, &attrs)
	v, _ := attrs[name].(string)
	return v
}

// deviceTools lists the control tools of the project's plugins that act on an entry.
func (s *Service) deviceTools(ctx context.Context, p store.Project, e store.Entity) []DeviceTool {
	aps, _ := s.activePlugins(ctx, p.ID)
	out := []DeviceTool{}
	for _, ap := range aps {
		for _, t := range ap.pkg.Manifest.Contributes.MCPTools {
			if !t.Control || t.Target == nil {
				continue
			}
			v := e.Slug
			if t.Target.Attr != "" {
				if v = attrString(e, t.Target.Attr); v == "" {
					continue
				}
			} else if !ownKind(ap, e.Kind) {
				continue
			}
			dt := DeviceTool{Plugin: ap.row.ID, PluginName: ap.pkg.Manifest.Name, Tool: t.Name, Description: t.Description, Arg: t.Target.Arg, Value: v}
			if b, ok := ap.pkg.File(t.Input); ok && t.Input != "" {
				dt.Input = b
			}
			out = append(out, dt)
		}
	}
	return out
}

func (s *Service) hasGrant(ctx context.Context, entityID, userID string) bool {
	gs, err := s.DB.DeviceGrants(ctx, entityID)
	if err != nil {
		return false
	}
	now := time.Now()
	return slices.ContainsFunc(gs, func(g store.ControlGrant) bool {
		return g.UserID == userID && (g.ExpiresAt.IsZero() || g.ExpiresAt.After(now))
	})
}

func (s *Service) aiMayControl(ctx context.Context, entityID string) bool {
	ds, err := s.DB.GetDeviceSetting(ctx, entityID)
	return err == nil && ds.AIControl == AIDirect
}

// Device returns the control tools and rules of an entry (readers; grants
// only for owners).
func (s *Service) Device(ctx context.Context, a Actor, ref, slug string) (DeviceInfo, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return DeviceInfo{}, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, slug)
	if err != nil {
		return DeviceInfo{}, err
	}
	info := DeviceInfo{Tools: s.deviceTools(ctx, p, e), AIControl: AISuggest}
	if ds, err := s.DB.GetDeviceSetting(ctx, e.ID); err == nil {
		info.AIControl = ds.AIControl
	}
	info.CanControl = len(info.Tools) > 0 && (rank[p.Role] >= rank[store.RoleEditor] && !s.readOnly(a, p) || s.hasGrant(ctx, e.ID, a.User.ID))
	info.CanManage = len(info.Tools) > 0 && p.Role == store.RoleOwner && a.Via != "mcp" && a.Plugin == "" && !a.Restricted()
	if info.CanManage {
		if info.Grants, err = s.DB.DeviceGrants(ctx, e.ID); err != nil {
			return info, err
		}
	}
	return info, nil
}

// manageDevice loads a device for its owner (people only).
func (s *Service) manageDevice(ctx context.Context, a Actor, ref, slug string) (store.Project, store.Entity, error) {
	if a.Via == "mcp" || a.Plugin != "" || a.Restricted() {
		return store.Project{}, store.Entity{}, ErrForbidden
	}
	p, err := s.project(ctx, a, ref, store.RoleOwner)
	if err != nil {
		return p, store.Entity{}, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, slug)
	if err != nil {
		return p, e, err
	}
	if len(s.deviceTools(ctx, p, e)) == 0 {
		return p, e, invalid("validation.not_a_device", nil, "no control tool acts on this entry")
	}
	return p, e, nil
}

// SetDeviceAI decides whether AI clients may switch a device without a
// person confirming each call (owners).
func (s *Service) SetDeviceAI(ctx context.Context, a Actor, ref, slug, mode string) (DeviceInfo, error) {
	if mode != AISuggest && mode != AIDirect {
		return DeviceInfo{}, invalid("validation.ai_control", nil, "aiControl must be suggest or direct")
	}
	p, e, err := s.manageDevice(ctx, a, ref, slug)
	if err != nil {
		return DeviceInfo{}, err
	}
	if err := s.DB.SaveDeviceSetting(ctx, store.DeviceSetting{EntityID: e.ID, ProjectID: p.ID, AIControl: mode, UpdatedBy: a.String(), UpdatedAt: store.Now()}); err != nil {
		return DeviceInfo{}, err
	}
	s.event(ctx, a, "device.ai_control", e.ID, map[string]any{"aiControl": mode})
	return s.Device(ctx, a, ref, slug)
}

// GrantControl lets one person switch a device (owners). The person needs
// an account on the instance; they see the device under "Shared devices".
func (s *Service) GrantControl(ctx context.Context, a Actor, ref, slug string, in DeviceGrantInput) (DeviceInfo, error) {
	p, e, err := s.manageDevice(ctx, a, ref, slug)
	if err != nil {
		return DeviceInfo{}, err
	}
	if in.Days < 0 || in.Days > 3660 {
		return DeviceInfo{}, invalid("validation.grant_days", map[string]any{"max": 3660}, "days must be between 0 and 3660")
	}
	u, err := s.DB.UserByEmail(ctx, strings.TrimSpace(in.Email))
	if errors.Is(err, store.ErrNotFound) {
		return DeviceInfo{}, invalid("validation.user_unknown", nil, "no account with this e-mail address")
	} else if err != nil {
		return DeviceInfo{}, err
	}
	g := store.ControlGrant{ID: newID(), ProjectID: p.ID, EntityID: e.ID, UserID: u.ID, GrantedBy: a.User.ID, CreatedAt: store.Now()}
	if in.Days > 0 {
		g.ExpiresAt = store.Time{Time: g.CreatedAt.AddDate(0, 0, in.Days)}
	}
	if err := s.DB.SaveControlGrant(ctx, g); err != nil {
		return DeviceInfo{}, err
	}
	s.event(ctx, a, "device.grant", e.ID, map[string]any{"user": u.ID, "days": in.Days})
	if u.ID != a.User.ID {
		s.notify(ctx, u, Note{Kind: "device.shared", ProjectID: p.ID, Project: p.Name, Subject: e.ID, Title: e.Name, Link: "/devices", Actor: a.User.Name})
	}
	return s.Device(ctx, a, ref, slug)
}

// RevokeControl removes a grant (owners).
func (s *Service) RevokeControl(ctx context.Context, a Actor, ref, slug, grantID string) (DeviceInfo, error) {
	_, e, err := s.manageDevice(ctx, a, ref, slug)
	if err != nil {
		return DeviceInfo{}, err
	}
	g, err := s.DB.GetControlGrant(ctx, grantID)
	if err != nil || g.EntityID != e.ID {
		return DeviceInfo{}, ErrNotFound
	}
	if err := s.DB.DeleteControlGrant(ctx, e.ID, grantID); err != nil {
		return DeviceInfo{}, err
	}
	s.event(ctx, a, "device.revoke", e.ID, map[string]any{"user": g.UserID})
	return s.Device(ctx, a, ref, slug)
}

// DeviceLog lists who switched a device and who changed its rules
// (members who may control it).
func (s *Service) DeviceLog(ctx context.Context, a Actor, ref, slug string) ([]AuditEvent, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, slug)
	if err != nil {
		return nil, err
	}
	if rank[p.Role] < rank[store.RoleEditor] && !s.hasGrant(ctx, e.ID, a.User.ID) {
		return nil, ErrForbidden
	}
	events, err := s.DB.Events(ctx, store.EventFilter{ProjectID: p.ID, Kind: "device.", Subject: e.ID, Limit: 100})
	if err != nil {
		return nil, err
	}
	return s.describeEvents(ctx, events), nil
}

// SharedDevices lists the devices shared with the signed-in person.
func (s *Service) SharedDevices(ctx context.Context, a Actor) ([]SharedDevice, error) {
	if a.Via == "mcp" || a.Plugin != "" {
		return nil, ErrForbidden
	}
	gs, err := s.DB.UserGrants(ctx, a.User.ID)
	if err != nil {
		return nil, err
	}
	out := []SharedDevice{}
	now := time.Now()
	for _, g := range gs {
		if !g.ExpiresAt.IsZero() && !g.ExpiresAt.After(now) {
			continue
		}
		p, e, err := s.grantDevice(ctx, a, g)
		if err != nil {
			continue
		}
		by := ""
		if u, err := s.DB.UserByID(ctx, g.GrantedBy); err == nil {
			by = u.Name
		}
		out = append(out, SharedDevice{Grant: g.ID, Entity: e.Slug, Name: e.Name, Kind: e.Kind, ProjectName: p.Name, SharedBy: by, ExpiresAt: g.ExpiresAt, Tools: s.deviceTools(ctx, p, e)})
	}
	return out, nil
}

func (s *Service) grantDevice(ctx context.Context, a Actor, g store.ControlGrant) (store.Project, store.Entity, error) {
	p, err := s.DB.ProjectFor(ctx, g.ProjectID, a.User.ID)
	if err != nil {
		return p, store.Entity{}, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, g.EntityID)
	return p, e, err
}

// ControlInput calls a control tool on a device; the target argument is filled in.
type ControlInput struct {
	Plugin string          `json:"plugin"`
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args"`
}

// ControlDevice switches a device of a project (editors and grantees).
func (s *Service) ControlDevice(ctx context.Context, a Actor, ref, slug string, in ControlInput) (json.RawMessage, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, err
	}
	e, err := s.DB.GetEntity(ctx, p.ID, slug)
	if err != nil {
		return nil, err
	}
	args, err := s.deviceArgs(ctx, p, e, in)
	if err != nil {
		return nil, err
	}
	return s.PluginTool(ctx, a, p.Slug, in.Plugin, in.Tool, args)
}

// ControlShared switches a device shared with the signed-in person; they
// need no rights in the project.
func (s *Service) ControlShared(ctx context.Context, a Actor, grantID string, in ControlInput) (json.RawMessage, error) {
	if a.Via == "mcp" || a.Plugin != "" {
		return nil, ErrForbidden
	}
	g, err := s.DB.GetControlGrant(ctx, grantID)
	if err != nil || g.UserID != a.User.ID || (!g.ExpiresAt.IsZero() && !g.ExpiresAt.After(time.Now())) {
		return nil, ErrNotFound
	}
	p, e, err := s.grantDevice(ctx, a, g)
	if err != nil {
		return nil, ErrNotFound
	}
	args, err := s.deviceArgs(ctx, p, e, in)
	if err != nil {
		return nil, err
	}
	ap, err := s.activePlugin(ctx, p.ID, in.Plugin)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(ap.pkg.Manifest.Contributes.MCPTools, func(t plugins.ToolDef) bool { return t.Name == in.Tool })
	if i < 0 {
		return nil, ErrNotFound
	}
	return s.runControl(ctx, a, p, ap, ap.pkg.Manifest.Contributes.MCPTools[i], args, &e)
}

// deviceArgs checks that the tool acts on the device and fills in its target.
func (s *Service) deviceArgs(ctx context.Context, p store.Project, e store.Entity, in ControlInput) (json.RawMessage, error) {
	for _, t := range s.deviceTools(ctx, p, e) {
		if t.Plugin != in.Plugin || t.Tool != in.Tool {
			continue
		}
		m := map[string]any{}
		if len(in.Args) > 0 {
			if err := json.Unmarshal(in.Args, &m); err != nil {
				return nil, invalid("validation.json", nil, "args must be a JSON object")
			}
		}
		m[t.Arg] = t.Value
		return json.Marshal(m)
	}
	return nil, ErrNotFound
}
