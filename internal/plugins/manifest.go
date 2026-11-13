package plugins

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// HostAPI is the major version of the host API this server implements.
// The previous major version stays supported once a new one exists.
const HostAPI = "1"

// StoreKeys are the built-in trusted signing keys of store.phoedox.com
// ("keyid:base64", comma separated). Further keys come from
// PHOEDOX_PLUGIN_KEYS.
const StoreKeys = ""

// SupportedHostAPIs lists the host API versions plugins may target.
var SupportedHostAPIs = []string{"1"}

// Manifest mirrors phoedox-plugin.json (phoedox-sdk: schema,
// go/pdk/manifest.go). The host keeps its own copy so it does not depend on
// an SDK release; the tests use the same example manifest as the SDK.
type Manifest struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	HostAPI      string          `json:"hostApi"`
	Engines      Engines         `json:"engines"`
	Description  string          `json:"description,omitempty"`
	Backend      *Backend        `json:"backend,omitempty"`
	Frontend     *Frontend       `json:"frontend,omitempty"`
	CriticalPath bool            `json:"criticalPath,omitempty"`
	Contributes  Contributes     `json:"contributes,omitempty"`
	Permissions  []string        `json:"permissions,omitempty"`
	License      json.RawMessage `json:"license,omitempty"`
}

type Engines struct {
	Phoedox string `json:"phoedox"`
}

type Backend struct {
	Wasm     string `json:"wasm"`
	MemoryMB int    `json:"memoryMb,omitempty"`
}

type Frontend struct {
	Module string `json:"module"`
}

type Contributes struct {
	Kinds     []KindDef     `json:"kinds,omitempty"`
	Relations []RelationDef `json:"relations,omitempty"`
	MCPTools  []ToolDef     `json:"mcpTools,omitempty"`
	Events    []string      `json:"events,omitempty"`
}

type KindDef struct {
	ID     string `json:"id"`
	Schema string `json:"schema,omitempty"`
	Icon   string `json:"icon,omitempty"`
}

type RelationDef struct {
	ID   string   `json:"id"`
	From []string `json:"from"`
	To   []string `json:"to"`
	Card string   `json:"card,omitempty"`
}

type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Input       string `json:"input,omitempty"` // JSON Schema file in the package
}

var (
	idRe      = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9-]+)+$`)
	semverRe  = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	permRe    = regexp.MustCompile(`^(graph|docs|assets|events|ai|ha):(read|write|use|emit|control)(:[a-z0-9_,]+)?$|^net:fetch:[a-z0-9.-]+$|^secrets:[a-z0-9_]+$`)
	kindRe    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	toolRe    = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)
	maxMemory = 256
)

// MinSchedule is the shortest interval a plugin may be woken up at.
const MinSchedule = 30 * time.Second

// ParseManifest decodes and validates a manifest.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("manifest: %w", err)
	}
	return m, m.Validate()
}

// Validate checks the fields the host relies on.
func (m Manifest) Validate() error {
	switch {
	case !idRe.MatchString(m.ID):
		return fmt.Errorf("manifest: id %q must be reverse-DNS like de.example.ups", m.ID)
	case m.Name == "":
		return fmt.Errorf("manifest: name is required")
	case !semverRe.MatchString(m.Version):
		return fmt.Errorf("manifest: version %q is not SemVer", m.Version)
	case m.HostAPI == "":
		return fmt.Errorf("manifest: hostApi is required")
	case m.Engines.Phoedox == "":
		return fmt.Errorf("manifest: engines.phoedox is required")
	}
	if _, err := ParseRange(m.Engines.Phoedox); err != nil {
		return fmt.Errorf("manifest: engines.phoedox: %w", err)
	}
	if m.Backend != nil && (m.Backend.MemoryMB < 0 || m.Backend.MemoryMB > maxMemory) {
		return fmt.Errorf("manifest: backend.memoryMb must be between 1 and %d", maxMemory)
	}
	for _, p := range m.Permissions {
		if !permRe.MatchString(p) {
			return fmt.Errorf("manifest: unknown permission %q", p)
		}
	}
	for _, k := range m.Contributes.Kinds {
		if !kindRe.MatchString(k.ID) {
			return fmt.Errorf("manifest: kind %q must be lowercase letters, digits and _", k.ID)
		}
	}
	for _, t := range m.Contributes.MCPTools {
		if !toolRe.MatchString(t.Name) || len(t.Name) > 60 {
			return fmt.Errorf("manifest: tool name %q is invalid", t.Name)
		}
	}
	for _, e := range m.Contributes.Events {
		if _, _, err := ParseEvent(e); err != nil {
			return err
		}
	}
	return nil
}

// Supported tells whether this server can run the plugin.
func (m Manifest) Supported(serverVersion string) error {
	if !slices.Contains(SupportedHostAPIs, m.HostAPI) {
		return fmt.Errorf("plugin needs host API %s, this server offers %s", m.HostAPI, strings.Join(SupportedHostAPIs, ", "))
	}
	r, err := ParseRange(m.Engines.Phoedox)
	if err != nil {
		return err
	}
	if !r.Match(serverVersion) {
		return fmt.Errorf("plugin needs Phoedox %s, this server is %s", m.Engines.Phoedox, serverVersion)
	}
	return nil
}

// Has tells whether the manifest requests exactly this permission.
func (m Manifest) Has(perm string) bool {
	return slices.Contains(m.Permissions, perm)
}

// CanRead tells whether area:read (or more) was granted.
func (m Manifest) CanRead(area string) bool {
	for _, p := range m.Permissions {
		if p == area+":read" || p == area+":write" || strings.HasPrefix(p, area+":write:") {
			return true
		}
	}
	return false
}

// CanWriteKind tells whether the plugin may change entities of a kind:
// "graph:write" allows every kind, "graph:write:a,b" only the listed ones.
func (m Manifest) CanWriteKind(kind string) bool {
	for _, p := range m.Permissions {
		if p == "graph:write" {
			return true
		}
		if rest, ok := strings.CutPrefix(p, "graph:write:"); ok && slices.Contains(strings.Split(rest, ","), kind) {
			return true
		}
	}
	return false
}

// CanFetch tells whether the plugin may call a host.
func (m Manifest) CanFetch(host string) bool {
	return m.Has("net:fetch:" + strings.ToLower(host))
}

// CanSecret tells whether the plugin may read a secret.
func (m Manifest) CanSecret(name string) bool {
	return m.Has("secrets:" + name)
}

// Secrets lists the secrets the plugin asks for.
func (m Manifest) Secrets() []string {
	var out []string
	for _, p := range m.Permissions {
		if name, ok := strings.CutPrefix(p, "secrets:"); ok {
			out = append(out, name)
		}
	}
	return out
}

// Schedule is the shortest "schedule:every=…" interval (0 = none).
func (m Manifest) Schedule() time.Duration {
	var best time.Duration
	for _, e := range m.Contributes.Events {
		if kind, every, err := ParseEvent(e); err == nil && kind == "schedule" && (best == 0 || every < best) {
			best = every
		}
	}
	return best
}

// Subscribes tells whether the plugin wants an event type
// ("entity.updated", "entity.*", "*").
func (m Manifest) Subscribes(typ string) bool {
	for _, e := range m.Contributes.Events {
		if kind, _, err := ParseEvent(e); err != nil || kind == "schedule" {
			continue
		}
		if e == "*" || e == typ || (strings.HasSuffix(e, ".*") && strings.HasPrefix(typ, strings.TrimSuffix(e, "*"))) {
			return true
		}
	}
	return false
}

var eventRe = regexp.MustCompile(`^(\*|[a-z_]+\.(\*|[a-z_]+))$`)

// ParseEvent checks one entry of contributes.events: either
// "schedule:every=<duration>" or an event type pattern.
func ParseEvent(e string) (kind string, every time.Duration, err error) {
	if rest, ok := strings.CutPrefix(e, "schedule:every="); ok {
		d, err := time.ParseDuration(rest)
		if err != nil || d < MinSchedule {
			return "", 0, fmt.Errorf("manifest: event %q needs a duration of at least %s", e, MinSchedule)
		}
		return "schedule", d, nil
	}
	if !eventRe.MatchString(e) {
		return "", 0, fmt.Errorf("manifest: unknown event %q", e)
	}
	return "event", 0, nil
}

// Version is a parsed SemVer version (pre-release only compared as a whole).
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

// ParseVersion reads "1.2.3" or "v1.2.3-rc.1".
func ParseVersion(s string) (Version, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	core, pre, _ := strings.Cut(s, "-")
	core, _, _ = strings.Cut(core, "+")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q is not SemVer", s)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			return Version{}, fmt.Errorf("version %q is not SemVer", s)
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2], pre}, nil
}

// Compare returns -1, 0 or 1. A pre-release sorts before its release.
func (v Version) Compare(o Version) int {
	for _, d := range [][2]int{{v.Major, o.Major}, {v.Minor, o.Minor}, {v.Patch, o.Patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case v.Pre == o.Pre:
		return 0
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	case v.Pre < o.Pre:
		return -1
	default:
		return 1
	}
}

// CompareVersions compares two version strings; invalid ones sort first.
func CompareVersions(a, b string) int {
	va, ea := ParseVersion(a)
	vb, eb := ParseVersion(b)
	switch {
	case ea != nil && eb != nil:
		return strings.Compare(a, b)
	case ea != nil:
		return -1
	case eb != nil:
		return 1
	}
	return va.Compare(vb)
}

// Range is a list of comparators that must all match (">=1.4.0 <2.0.0").
type Range []comparator

type comparator struct {
	op string
	v  Version
}

// ParseRange reads space separated comparators with >=, >, <=, <, = or none.
func ParseRange(s string) (Range, error) {
	var r Range
	for _, f := range strings.Fields(s) {
		op := ""
		for _, o := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(f, o) {
				op = o
				break
			}
		}
		v, err := ParseVersion(strings.TrimPrefix(f, op))
		if err != nil {
			return nil, err
		}
		if op == "" {
			op = "="
		}
		r = append(r, comparator{op, v})
	}
	if len(r) == 0 {
		return nil, fmt.Errorf("empty version range")
	}
	return r, nil
}

// Match tells whether a server version lies in the range. Development builds
// ("dev" or anything that is not SemVer) match every range.
func (r Range) Match(version string) bool {
	v, err := ParseVersion(version)
	if err != nil {
		return true
	}
	v.Pre = "" // a release candidate of 1.4.0 counts as 1.4.0
	for _, c := range r {
		d := v.Compare(c.v)
		ok := map[string]bool{">=": d >= 0, ">": d > 0, "<=": d <= 0, "<": d < 0, "=": d == 0}[c.op]
		if !ok {
			return false
		}
	}
	return true
}
