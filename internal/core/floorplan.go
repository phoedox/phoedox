package core

import (
	"context"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/phoedox/phoedox/internal/store"
)

// Floor plans (ADR-0034): a document of type "floorplan" holds rooms as
// polygons and devices as points, all in millimetres. Rooms and devices refer
// to entries; saving the plan keeps the graph in step (devices located_in the
// room they stand in, rooms part_of the floor, room areas).

// Floorplan is the content of a floor plan document.
type Floorplan struct {
	Description string `json:"description,omitempty"`
	// Floor is the slug of a floor entry the rooms are part of.
	Floor string `json:"floor,omitempty"`
	// Image is an optional background (a project file id) drawn from the
	// origin; ImageWidth is its width in millimetres, set by calibration.
	Image      string  `json:"image,omitempty"`
	ImageWidth float64 `json:"imageWidth,omitempty"`
	// Width and Height are the drawing area in millimetres.
	Width   float64       `json:"width"`
	Height  float64       `json:"height"`
	Rooms   []FloorRoom   `json:"rooms"`
	Devices []FloorDevice `json:"devices"`
}

// FloorRoom is a room polygon.
type FloorRoom struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Entity string       `json:"entity,omitempty"`
	Color  string       `json:"color,omitempty"`
	Points [][2]float64 `json:"points"`
}

// FloorDevice places an entry on the plan.
type FloorDevice struct {
	ID     string  `json:"id"`
	Entity string  `json:"entity"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

var (
	planIDRe    = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)
	planColorRe = regexp.MustCompile(`^(gray|red|orange|yellow|green|blue|purple|pink)?$`)
)

const (
	maxPlanSize   = 1_000_000 // 1 km in mm
	maxPlanRooms  = 500
	maxPlanDevs   = 2000
	maxRoomPoints = 200
)

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func floorplanContent(fp *Floorplan) (store.JSON, string, error) {
	if fp == nil {
		fp = &Floorplan{}
	}
	if fp.Width <= 0 {
		fp.Width = 20000
	}
	if fp.Height <= 0 {
		fp.Height = 15000
	}
	bad := func(what string) error {
		return invalid("validation.floorplan", map[string]any{"what": what}, "floor plan: invalid %s", what)
	}
	if !finite(fp.Width) || !finite(fp.Height) || fp.Width > maxPlanSize || fp.Height > maxPlanSize {
		return nil, "", bad("size")
	}
	if !finite(fp.ImageWidth) || fp.ImageWidth < 0 || fp.ImageWidth > maxPlanSize {
		return nil, "", bad("imageWidth")
	}
	if len(fp.Rooms) > maxPlanRooms || len(fp.Devices) > maxPlanDevs {
		return nil, "", bad("count")
	}
	if fp.Rooms == nil {
		fp.Rooms = []FloorRoom{}
	}
	if fp.Devices == nil {
		fp.Devices = []FloorDevice{}
	}
	inside := func(x, y float64) bool {
		return finite(x) && finite(y) && x >= -maxPlanSize && y >= -maxPlanSize && x <= maxPlanSize && y <= maxPlanSize
	}
	ids := map[string]bool{}
	var refs []string
	for i := range fp.Rooms {
		r := &fp.Rooms[i]
		r.Name = strings.TrimSpace(r.Name)
		if !planIDRe.MatchString(r.ID) || ids[r.ID] || len(r.Points) < 3 || len(r.Points) > maxRoomPoints || len(r.Name) > 200 || !planColorRe.MatchString(r.Color) {
			return nil, "", bad("room")
		}
		ids[r.ID] = true
		for _, pt := range r.Points {
			if !inside(pt[0], pt[1]) {
				return nil, "", bad("room")
			}
		}
		if r.Entity != "" {
			refs = append(refs, r.Entity)
		}
	}
	for i := range fp.Devices {
		d := &fp.Devices[i]
		if !planIDRe.MatchString(d.ID) || ids[d.ID] || d.Entity == "" || !inside(d.X, d.Y) {
			return nil, "", bad("device")
		}
		ids[d.ID] = true
		refs = append(refs, d.Entity)
	}
	if fp.Floor != "" {
		refs = append(refs, fp.Floor)
	}
	j, _ := json.Marshal(fp)
	// Mentions link the plan to every entry on it.
	text := fp.Description
	for _, r := range refs {
		if planIDRe.MatchString(r) {
			text += " @" + r
		}
	}
	return j, text, nil
}

// polygonArea is the area of a polygon (shoelace formula).
func polygonArea(pts [][2]float64) float64 {
	a := 0.0
	for i := range pts {
		j := (i + 1) % len(pts)
		a += pts[i][0]*pts[j][1] - pts[j][0]*pts[i][1]
	}
	return math.Abs(a) / 2
}

// inPolygon reports whether (x, y) lies inside the polygon (ray casting).
func inPolygon(x, y float64, pts [][2]float64) bool {
	in := false
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		xi, yi, xj, yj := pts[i][0], pts[i][1], pts[j][0], pts[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

// RoomAt returns the room containing the point; the smallest one wins when
// rooms overlap.
func (fp Floorplan) RoomAt(x, y float64) (FloorRoom, bool) {
	var best FloorRoom
	found, bestArea := false, math.MaxFloat64
	for _, r := range fp.Rooms {
		if inPolygon(x, y, r.Points) {
			if a := polygonArea(r.Points); a < bestArea {
				best, bestArea, found = r, a, true
			}
		}
	}
	return best, found
}

const floorplanSource = "floorplan"

// syncFloorplan brings the graph in line with a saved plan. It only adds
// what the plan shows and removes edges it added itself earlier; edges people
// drew by hand stay.
func (s *Service) syncFloorplan(ctx context.Context, a Actor, p store.Project, doc store.Document) {
	var fp Floorplan
	if json.Unmarshal(doc.Content, &fp) != nil {
		return
	}
	m, err := s.model(ctx, p.ID)
	if err != nil {
		return
	}
	get := func(slug string) (store.Entity, bool) {
		if slug == "" {
			return store.Entity{}, false
		}
		e, err := s.DB.GetEntity(ctx, p.ID, slug)
		return e, err == nil
	}
	linked, unlinked := 0, 0
	// ensure links from → to with rel and drops older plan edges of that rel.
	ensure := func(from store.Entity, rel string, to store.Entity, ok bool) {
		edges, _ := s.DB.EdgesOf(ctx, from.ID)
		have := false
		for _, e := range edges {
			if e.From != from.ID || e.Rel != rel {
				continue
			}
			if ok && e.To == to.ID {
				have = true
			} else if e.Source == floorplanSource {
				if s.DB.UnlinkEntities(ctx, e.From, rel, e.To) == nil {
					unlinked++
				}
			}
		}
		if ok && !have && slices.Contains(m.AllowedRels(from.Kind, to.Kind), rel) {
			if _, err := s.DB.LinkEntities(ctx, store.Edge{ID: newID(), From: from.ID, Rel: rel, To: to.ID, Attrs: []byte("{}"), Source: floorplanSource}); err == nil {
				linked++
			}
		}
	}
	floor, hasFloor := get(fp.Floor)
	for _, r := range fp.Rooms {
		room, ok := get(r.Entity)
		if !ok {
			continue
		}
		ensure(room, "part_of", floor, hasFloor)
		if room.Kind == "room" {
			attrs := map[string]any{}
			_ = json.Unmarshal(room.Attrs, &attrs)
			area := math.Round(polygonArea(r.Points)/1e5) / 10 // mm² → m², one decimal
			if cur, _ := attrs["area"].(float64); cur != area {
				attrs["area"] = area
				raw, _ := json.Marshal(attrs)
				room.Attrs = raw
				_, _, _ = s.DB.UpsertEntity(ctx, room)
			}
		}
	}
	for _, d := range fp.Devices {
		dev, ok := get(d.Entity)
		if !ok {
			continue
		}
		r, in := fp.RoomAt(d.X, d.Y)
		room, ok := get(r.Entity)
		ensure(dev, "located_in", room, in && ok)
	}
	if linked+unlinked > 0 {
		s.event(ctx, a, "floorplan.sync", doc.ID, map[string]int{"linked": linked, "unlinked": unlinked})
	}
}
