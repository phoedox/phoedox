package core

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path"
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
	// Walls, their openings (doors, windows) and photos placed on the plan
	// (stage 2). Older plans have none.
	Walls    []FloorWall    `json:"walls,omitempty"`
	Openings []FloorOpening `json:"openings,omitempty"`
	Photos   []FloorPhoto   `json:"photos,omitempty"`
	Cables   []FloorCable   `json:"cables,omitempty"`
}

// FloorCable is a cable route: points with x, y and the height above this
// floor (mm; above the ceiling it runs into the storey above, below 0 into
// the one beneath). From and To are the entries at its ends.
type FloorCable struct {
	ID     string       `json:"id"`
	Kind   string       `json:"kind"` // network | power | coax | other
	Label  string       `json:"label,omitempty"`
	Points [][3]float64 `json:"points"`
	From   string       `json:"from,omitempty"`
	To     string       `json:"to,omitempty"`
	Entity string       `json:"entity,omitempty"` // the cable itself as an entry, optional
}

// FloorWall is a straight wall from A to B (centre line).
type FloorWall struct {
	ID        string     `json:"id"`
	A         [2]float64 `json:"a"`
	B         [2]float64 `json:"b"`
	Thickness float64    `json:"thickness"`        // mm
	Height    float64    `json:"height,omitempty"` // mm, 0 = plan default
}

// FloorOpening is a door or window in a wall.
type FloorOpening struct {
	ID     string  `json:"id"`
	Wall   string  `json:"wall"`
	Kind   string  `json:"kind"`   // door or window
	Offset float64 `json:"offset"` // mm from the wall's start to the opening's centre
	Width  float64 `json:"width"`  // mm
}

// FloorPhoto places a project image on the plan.
type FloorPhoto struct {
	ID      string  `json:"id"`
	File    string  `json:"file"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Caption string  `json:"caption,omitempty"`
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
	maxPlanWalls  = 5000
	maxPlanOpen   = 2000
	maxPlanPhotos = 500
	maxPlanCables = 1000
	maxCablePts   = 200
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
	if len(fp.Rooms) > maxPlanRooms || len(fp.Devices) > maxPlanDevs || len(fp.Walls) > maxPlanWalls || len(fp.Openings) > maxPlanOpen || len(fp.Photos) > maxPlanPhotos || len(fp.Cables) > maxPlanCables {
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
	walls := map[string]float64{} // id → length
	for i := range fp.Walls {
		w := &fp.Walls[i]
		if w.Thickness == 0 {
			w.Thickness = 120
		}
		length := math.Hypot(w.B[0]-w.A[0], w.B[1]-w.A[1])
		if !planIDRe.MatchString(w.ID) || ids[w.ID] || !inside(w.A[0], w.A[1]) || !inside(w.B[0], w.B[1]) || !(length > 0) ||
			!finite(w.Thickness) || w.Thickness < 10 || w.Thickness > 2000 || !finite(w.Height) || w.Height < 0 || w.Height > 100_000 {
			return nil, "", bad("wall")
		}
		ids[w.ID] = true
		walls[w.ID] = length
	}
	for i := range fp.Openings {
		o := &fp.Openings[i]
		length, ok := walls[o.Wall]
		if !planIDRe.MatchString(o.ID) || ids[o.ID] || !ok || (o.Kind != "door" && o.Kind != "window") ||
			!finite(o.Width) || o.Width < 100 || o.Width > length || !finite(o.Offset) || o.Offset-o.Width/2 < -1 || o.Offset+o.Width/2 > length+1 {
			return nil, "", bad("opening")
		}
		ids[o.ID] = true
	}
	cableKinds := map[string]bool{"network": true, "power": true, "coax": true, "other": true}
	for i := range fp.Cables {
		c := &fp.Cables[i]
		c.Label = strings.TrimSpace(c.Label)
		if c.Kind == "" {
			c.Kind = "network"
		}
		if !planIDRe.MatchString(c.ID) || ids[c.ID] || !cableKinds[c.Kind] || len(c.Points) < 2 || len(c.Points) > maxCablePts || len(c.Label) > 200 {
			return nil, "", bad("cable")
		}
		for _, pt := range c.Points {
			if !inside(pt[0], pt[1]) || !finite(pt[2]) || pt[2] < -20000 || pt[2] > 100000 {
				return nil, "", bad("cable")
			}
		}
		for _, r := range []string{c.From, c.To, c.Entity} {
			if r != "" {
				refs = append(refs, r)
			}
		}
		ids[c.ID] = true
	}
	for i := range fp.Photos {
		ph := &fp.Photos[i]
		ph.Caption = strings.TrimSpace(ph.Caption)
		if !planIDRe.MatchString(ph.ID) || ids[ph.ID] || !planIDRe.MatchString(ph.File) || !inside(ph.X, ph.Y) || len(ph.Caption) > 500 {
			return nil, "", bad("photo")
		}
		ids[ph.ID] = true
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
	// ensureEdge adds an edge (never removes: one entry can have several cables).
	ensureEdge := func(from store.Entity, rel string, to store.Entity) {
		edges, _ := s.DB.EdgesOf(ctx, from.ID)
		for _, e := range edges {
			if e.From == from.ID && e.Rel == rel && e.To == to.ID {
				return
			}
		}
		if _, err := s.DB.LinkEntities(ctx, store.Edge{ID: newID(), From: from.ID, Rel: rel, To: to.ID, Attrs: []byte("{}"), Source: floorplanSource}); err == nil {
			linked++
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
	// Cables link their ends: network cables wired_to, power cables
	// powered_by (in whichever direction the data model allows).
	for _, c := range fp.Cables {
		rel := map[string]string{"network": "wired_to", "power": "powered_by"}[c.Kind]
		from, ok1 := get(c.From)
		to, ok2 := get(c.To)
		if rel == "" || !ok1 || !ok2 || from.ID == to.ID {
			continue
		}
		switch {
		case slices.Contains(m.AllowedRels(from.Kind, to.Kind), rel):
			ensureEdge(from, rel, to)
		case slices.Contains(m.AllowedRels(to.Kind, from.Kind), rel):
			ensureEdge(to, rel, from)
		}
	}
	if linked+unlinked > 0 {
		s.event(ctx, a, "floorplan.sync", doc.ID, map[string]int{"linked": linked, "unlinked": unlinked})
	}
}

// planFiles checks that the background and photos are files of the project.
func (s *Service) planFiles(ctx context.Context, p store.Project, fp *Floorplan) error {
	if fp == nil {
		return nil
	}
	ids := []string{}
	if fp.Image != "" {
		ids = append(ids, fp.Image)
	}
	for _, ph := range fp.Photos {
		ids = append(ids, ph.File)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		f, err := s.DB.GetFile(ctx, id)
		if err != nil || f.ProjectID != p.ID {
			return invalid("validation.floorplan", map[string]any{"what": "file"}, "floor plan: unknown file %s", id)
		}
	}
	return nil
}

// planImagePixels is the size of a PDF page rendered as plan background.
const planImagePixels = 4096

// PlanImage turns an uploaded PDF into a PNG for a floor plan background
// (the first page; editors). Images are returned as they are.
func (s *Service) PlanImage(ctx context.Context, a Actor, ref, id string) (store.File, error) {
	f, err := s.fileIn(ctx, a, ref, id, store.RoleEditor)
	if err != nil {
		return f, err
	}
	if strings.HasPrefix(f.Mime, "image/") {
		return f, nil
	}
	if f.Mime != "application/pdf" {
		return f, invalid("validation.plan_image", nil, "the background must be an image or a PDF")
	}
	p, err := s.project(ctx, a, ref, store.RoleEditor)
	if err != nil {
		return f, err
	}
	src, err := s.blobToTemp(ctx, f.SHA256)
	if err != nil {
		return f, err
	}
	defer os.Remove(src)
	png, err := s.Preview.PDFPage(ctx, src, planImagePixels)
	if err != nil {
		slog.Warn("plan image", "file", f.ID, "err", err)
		return f, invalid("validation.plan_pdf", nil, "the PDF could not be turned into an image on this server")
	}
	sp, err := s.spool(bytes.NewReader(png))
	if err != nil {
		return f, err
	}
	defer os.Remove(sp.path)
	return s.ingest(ctx, a, p, sp, cleanName(strings.TrimSuffix(f.Name, path.Ext(f.Name))+".png"), f.DocumentID, f.EntityID, nil)
}
