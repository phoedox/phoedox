package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Attribute types of kinds.
var attrTypes = []string{"text", "number", "bool", "date", "ip", "url", "select"}

// KindIcons are the Lucide icons a project may pick for its own kinds; the
// web app bundles exactly these (plus the icons of the core kinds).
var KindIcons = []string{"Shapes", "Tag", "CircleDot", "Box", "Boxes", "Cpu", "Package", "Workflow", "Database", "Server", "Monitor",
	"Plug", "PlugZap", "Lightbulb", "Router", "Wifi", "Network", "Cable", "Phone", "UserRound", "Layers", "DoorOpen", "Container"}

// Attr is one attribute of a kind.
type Attr struct {
	Name     string   `json:"name"`
	Label    string   `json:"label,omitempty"` // only for attributes defined by a project
	Type     string   `json:"type"`
	Unit     string   `json:"unit,omitempty"`
	Options  []string `json:"options,omitempty"`
	Required bool     `json:"required,omitempty"`
	Project  bool     `json:"project,omitempty"` // added by the project (read only)
}

// Relation allows edges rel from a kind to the listed kinds ("*" = any).
type Relation struct {
	Rel     string   `json:"rel"`
	To      []string `json:"to"`
	Project bool     `json:"project,omitempty"` // added by the project (read only)
}

// Kind describes a kind of entity: its attributes and allowed relations.
type Kind struct {
	ID          string     `json:"id"`
	Name        string     `json:"name,omitempty"` // own kinds; core kinds are named by the client (i18n)
	Description string     `json:"description,omitempty"`
	Group       string     `json:"group"`
	Icon        string     `json:"icon"`
	Source      string     `json:"source"` // core | project | plugin:<id>
	Attrs       []Attr     `json:"attrs"`
	Relations   []Relation `json:"relations,omitempty"`
	Count       int        `json:"count"`
}

// Model is the data model of a project: core kinds with project extensions
// plus the project's own kinds.
type Model struct {
	Kinds   []Kind         `json:"kinds"`
	Generic []string       `json:"generic"`           // relations allowed between any kinds
	Unknown map[string]int `json:"unknown,omitempty"` // entity kinds without definition (older data)
}

// KindInput defines an own kind or extends a core kind (then only attrs and
// relations count).
type KindInput struct {
	ID          string     `json:"id"`
	Name        string     `json:"name,omitempty"`
	Description string     `json:"description,omitempty"`
	Icon        string     `json:"icon,omitempty"`
	Attrs       []Attr     `json:"attrs,omitempty"`
	Relations   []Relation `json:"relations,omitempty"`
}

// KindResult is either an applied change or a pending suggestion.
type KindResult struct {
	Applied    bool              `json:"applied"`
	Kind       *Kind             `json:"kind,omitempty"`
	Suggestion *store.Suggestion `json:"suggestion,omitempty"`
}

type coreModel struct {
	Generic []string `json:"generic"`
	Kinds   []Kind   `json:"kinds"`
}

var loadCore = sync.OnceValue(func() coreModel {
	var m coreModel
	if err := json.Unmarshal(resources.CoreKinds(), &m); err != nil {
		panic("core kinds: " + err.Error())
	}
	for i := range m.Kinds {
		m.Kinds[i].Source = "core"
	}
	return m
})

func coreKind(id string) (Kind, bool) {
	for _, k := range loadCore().Kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

// Find returns the kind with the id.
func (m Model) Find(id string) (Kind, bool) {
	for _, k := range m.Kinds {
		if k.ID == id {
			return k, true
		}
	}
	return Kind{}, false
}

// AllowedRels lists the relations allowed from one kind to another.
func (m Model) AllowedRels(from, to string) []string {
	out := slices.Clone(m.Generic)
	if k, ok := m.Find(from); ok {
		for _, r := range k.Relations {
			if slices.Contains(r.To, to) || slices.Contains(r.To, "*") {
				if !slices.Contains(out, r.Rel) {
					out = append(out, r.Rel)
				}
			}
		}
	}
	return out
}

// DataModel returns the data model of a project.
func (s *Service) DataModel(ctx context.Context, a Actor, ref string) (Model, error) {
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return Model{}, err
	}
	m, err := s.model(ctx, p.ID)
	if err != nil {
		return m, err
	}
	counts, err := s.DB.CountEntitiesByKind(ctx, p.ID)
	if err != nil {
		return m, err
	}
	for i := range m.Kinds {
		m.Kinds[i].Count = counts[m.Kinds[i].ID]
		delete(counts, m.Kinds[i].ID)
	}
	if len(counts) > 0 {
		m.Unknown = counts
	}
	return m, nil
}

func (s *Service) model(ctx context.Context, projectID string) (Model, error) {
	c := loadCore()
	m := Model{Generic: c.Generic}
	defs, err := s.DB.ProjectKinds(ctx, projectID)
	if err != nil {
		return m, err
	}
	ext := map[string]KindInput{}
	var own []Kind
	for _, d := range defs {
		var in KindInput
		if json.Unmarshal(d.Def, &in) != nil {
			continue
		}
		if _, ok := coreKind(d.Kind); ok {
			ext[d.Kind] = in
			continue
		}
		if in.Attrs == nil {
			in.Attrs = []Attr{} // clients rely on a list
		}
		own = append(own, Kind{ID: d.Kind, Name: in.Name, Description: in.Description, Group: "custom", Icon: in.Icon, Source: "project",
			Attrs: in.Attrs, Relations: in.Relations})
	}
	for _, k := range c.Kinds {
		k.Attrs, k.Relations = slices.Clone(k.Attrs), slices.Clone(k.Relations)
		if in, ok := ext[k.ID]; ok {
			for _, at := range in.Attrs {
				at.Project = true
				k.Attrs = append(k.Attrs, at)
			}
			for _, r := range in.Relations {
				r.Project = true
				k.Relations = append(k.Relations, r)
			}
		}
		m.Kinds = append(m.Kinds, k)
	}
	sort.Slice(own, func(i, j int) bool { return strings.ToLower(own[i].Name) < strings.ToLower(own[j].Name) })
	m.Kinds = append(m.Kinds, own...)
	// Kinds of plugins come last; a kind the project or core defines wins.
	for _, pk := range s.pluginKinds(ctx, projectID) {
		if _, taken := m.Find(pk.ID); !taken {
			m.Kinds = append(m.Kinds, pk)
		}
	}
	return m, nil
}

func validName(s string) bool { return validRel(s) && s[0] >= 'a' && s[0] <= 'z' }

// normalize checks a kind definition against the current model.
func (in *KindInput) normalize(m Model) error {
	in.ID = strings.TrimSpace(in.ID)
	if !validName(in.ID) {
		return invalid("validation.kind", nil, "kind must be lowercase letters, digits and _")
	}
	core, isCore := coreKind(in.ID)
	if isCore {
		in.Name, in.Description, in.Icon = "", "", ""
	} else {
		in.Name = strings.TrimSpace(in.Name)
		in.Description = strings.TrimSpace(in.Description)
		if in.Name == "" || len(in.Name) > 60 {
			return invalid("validation.kind_name", nil, "name is required (at most 60 characters)")
		}
		if len(in.Description) > 500 {
			return invalid("validation.too_long", map[string]any{"field": "description", "max": 500}, "description is too long")
		}
		if in.Icon == "" {
			in.Icon = "Shapes"
		}
		if !slices.Contains(KindIcons, in.Icon) {
			return invalid("validation.kind_icon", map[string]any{"icons": strings.Join(KindIcons, ", ")}, "icon must be one of %s", strings.Join(KindIcons, ", "))
		}
	}
	if len(in.Attrs) > 50 || len(in.Relations) > 50 {
		return invalid("validation.too_many", map[string]any{"max": 50}, "at most 50 attributes and 50 relations")
	}
	seen := map[string]bool{}
	for _, at := range core.Attrs {
		seen[at.Name] = true
	}
	for i := range in.Attrs {
		at := &in.Attrs[i]
		at.Name, at.Label, at.Unit, at.Project = strings.TrimSpace(at.Name), strings.TrimSpace(at.Label), strings.TrimSpace(at.Unit), false
		if !validName(at.Name) {
			return invalid("validation.attr_name", map[string]any{"attr": at.Name}, "attribute %q: lowercase letters, digits and _", at.Name)
		}
		if seen[at.Name] {
			return invalid("validation.attr_duplicate", map[string]any{"attr": at.Name}, "attribute %q exists already", at.Name)
		}
		seen[at.Name] = true
		if !slices.Contains(attrTypes, at.Type) {
			return invalid("validation.attr_kind", map[string]any{"attr": at.Name, "types": strings.Join(attrTypes, ", ")}, "attribute %q: type must be one of %s", at.Name, strings.Join(attrTypes, ", "))
		}
		if len(at.Label) > 60 || len(at.Unit) > 12 {
			return invalid("validation.too_long", map[string]any{"field": at.Name, "max": 60}, "attribute %q: label or unit too long", at.Name)
		}
		if at.Type == "select" {
			var opts []string
			for _, o := range at.Options {
				if o = strings.TrimSpace(o); o != "" && !slices.Contains(opts, o) {
					opts = append(opts, o)
				}
			}
			if len(opts) == 0 || len(opts) > 50 {
				return invalid("validation.attr_options", map[string]any{"attr": at.Name}, "attribute %q: select needs 1 to 50 options", at.Name)
			}
			at.Options = opts
		} else {
			at.Options = nil
		}
	}
	for i := range in.Relations {
		r := &in.Relations[i]
		r.Rel, r.Project = strings.TrimSpace(r.Rel), false
		if !validName(r.Rel) {
			return invalid("validation.rel", nil, "rel must be lowercase letters, digits and _, e.g. located_in")
		}
		if len(r.To) == 0 {
			return invalid("validation.rel_targets", map[string]any{"rel": r.Rel}, "relation %q needs at least one target kind", r.Rel)
		}
		for _, to := range r.To {
			if _, ok := m.Find(to); !ok && to != "*" && to != in.ID {
				return invalid("validation.kind_unknown", map[string]any{"kind": to}, "unknown kind %q", to)
			}
		}
	}
	return nil
}

// DefineKind creates or replaces an own kind, or the project's extension of a core kind.
func (s *Service) DefineKind(ctx context.Context, a Actor, ref string, in KindInput) (KindResult, error) {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return KindResult{}, err
	}
	m, err := s.model(ctx, p.ID)
	if err != nil {
		return KindResult{}, err
	}
	if k, ok := m.Find(strings.TrimSpace(in.ID)); ok && strings.HasPrefix(k.Source, "plugin:") {
		return KindResult{}, invalid("validation.kind_plugin", map[string]any{"plugin": strings.TrimPrefix(k.Source, "plugin:")}, "the kind belongs to plugin %s", strings.TrimPrefix(k.Source, "plugin:"))
	}
	if err := in.normalize(m); err != nil {
		return KindResult{}, err
	}
	summary := "Art „" + in.ID + "“ festlegen"
	if sug, ok, err := s.maybeSuggest(ctx, a, p, "kind.define", summary, opPayload{Project: p.ID, Kind: &in}); ok || err != nil {
		return KindResult{Suggestion: sug}, err
	}
	return s.defineKind(ctx, a, p, in)
}

func (s *Service) defineKind(ctx context.Context, a Actor, p store.Project, in KindInput) (KindResult, error) {
	def, _ := json.Marshal(in)
	if err := s.DB.SetProjectKind(ctx, store.ProjectKind{ProjectID: p.ID, Kind: in.ID, Def: def, UpdatedBy: a.User.ID}); err != nil {
		return KindResult{}, err
	}
	s.event(ctx, a, "kind.define", p.ID, in)
	m, err := s.model(ctx, p.ID)
	if err != nil {
		return KindResult{}, err
	}
	k, _ := m.Find(in.ID)
	return KindResult{Applied: true, Kind: &k}, nil
}

// DeleteKind removes an own kind (only without entities) or a core kind's extension.
func (s *Service) DeleteKind(ctx context.Context, a Actor, ref, kind string) error {
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return err
	}
	if a.Via == "mcp" {
		return ErrForbidden
	}
	if _, isCore := coreKind(kind); !isCore {
		counts, err := s.DB.CountEntitiesByKind(ctx, p.ID)
		if err != nil {
			return err
		}
		if n := counts[kind]; n > 0 {
			return invalid("validation.kind_in_use", map[string]any{"count": n}, "%d entities still use this kind", n)
		}
	}
	if err := s.DB.DeleteProjectKind(ctx, p.ID, kind); err != nil {
		return err
	}
	s.event(ctx, a, "kind.delete", p.ID, map[string]string{"kind": kind})
	return nil
}

// checkAttrs validates and normalizes attribute values against a kind.
// Values of unknown attributes are kept as they are; empty values are dropped.
func checkAttrs(k Kind, attrs map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for name, v := range attrs {
		if v == nil || v == "" {
			continue
		}
		out[name] = v
	}
	for _, at := range k.Attrs {
		v, ok := out[at.Name]
		if !ok {
			if at.Required {
				return nil, invalid("validation.attr_required", map[string]any{"attr": at.Name}, "attribute %q is required for %s", at.Name, k.ID)
			}
			continue
		}
		nv, err := attrValue(at, v)
		if err != nil {
			return nil, err
		}
		out[at.Name] = nv
	}
	return out, nil
}

func attrValue(at Attr, v any) (any, error) {
	bad := func() error {
		return invalid("validation.attr_type", map[string]any{"attr": at.Name, "type": at.Type}, "attribute %q must be of type %s", at.Name, at.Type)
	}
	str, isStr := v.(string)
	if isStr {
		str = strings.TrimSpace(str)
	}
	switch at.Type {
	case "number":
		switch n := v.(type) {
		case float64:
			return n, nil
		case int:
			return float64(n), nil
		case json.Number:
			return n.Float64()
		case string:
			f, err := strconv.ParseFloat(strings.ReplaceAll(str, ",", "."), 64)
			if err != nil {
				return nil, bad()
			}
			return f, nil
		}
		return nil, bad()
	case "bool":
		if b, ok := v.(bool); ok {
			return b, nil
		}
		if b, err := strconv.ParseBool(str); isStr && err == nil {
			return b, nil
		}
		return nil, bad()
	}
	if !isStr {
		if at.Type != "text" {
			return nil, bad()
		}
		str = fmt.Sprint(v)
	}
	if len(str) > 2000 {
		return nil, invalid("validation.too_long", map[string]any{"field": at.Name, "max": 2000}, "attribute %q is too long", at.Name)
	}
	switch at.Type {
	case "date":
		if _, err := time.Parse(time.DateOnly, str); err != nil {
			return nil, bad()
		}
	case "ip":
		if _, err := netip.ParseAddr(str); err != nil {
			if _, err := netip.ParsePrefix(str); err != nil {
				return nil, bad()
			}
		}
	case "url":
		u, err := url.Parse(str)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, bad()
		}
	case "select":
		if !slices.Contains(at.Options, str) {
			return nil, invalid("validation.attr_option", map[string]any{"attr": at.Name, "options": strings.Join(at.Options, ", ")}, "attribute %q must be one of %s", at.Name, strings.Join(at.Options, ", "))
		}
	}
	return str, nil
}

// JSONSchema describes the attributes of a kind for AI clients.
func (k Kind) JSONSchema() map[string]any {
	props := map[string]any{}
	var required []string
	for _, at := range k.Attrs {
		p := map[string]any{}
		switch at.Type {
		case "number":
			p["type"] = "number"
		case "bool":
			p["type"] = "boolean"
		case "date":
			p["type"], p["format"] = "string", "date"
		case "ip":
			p["type"], p["description"] = "string", "IPv4/IPv6 address or prefix"
		case "url":
			p["type"], p["format"] = "string", "uri"
		case "select":
			p["type"], p["enum"] = "string", at.Options
		default:
			p["type"] = "string"
		}
		if at.Unit != "" {
			p["description"] = "unit: " + at.Unit
		}
		if at.Label != "" {
			p["title"] = at.Label
		}
		props[at.Name] = p
		if at.Required {
			required = append(required, at.Name)
		}
	}
	out := map[string]any{"$id": "phoedox:kind/" + k.ID, "type": "object", "properties": props}
	if k.Name != "" {
		out["title"] = k.Name
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}
