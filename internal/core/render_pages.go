package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"time"

	"github.com/phoedox/phoedox/internal/render"
	"github.com/phoedox/phoedox/internal/store"
)

// Pictures and PDFs of Phoedox pages made by the server-side browser
// (ADR-0095). It opens the page with a render pass of the person who asked:
// read-only, this project only, valid for two minutes.

// ErrRendererOff: the server-side browser is not on for this use.
var ErrRendererOff = errors.New("the server-side browser is off for this")

// renderReady is true once a render page has drawn everything.
const renderReady = `document.documentElement.dataset.ready === "1"`

// renderPage opens path (with query) on Phoedox for the actor and returns a
// picture (pdf false) or a PDF.
func (s *Service) renderPage(ctx context.Context, a Actor, use string, p store.Project, path string, o render.PageOptions, pdf bool) ([]byte, error) {
	r := s.renderer(ctx, use)
	if r == nil {
		return nil, ErrRendererOff
	}
	target := s.rendererTarget()
	if target == "" {
		return nil, errors.New("render: no address of Phoedox for the browser (PHOEDOX_RENDERER_TARGET or PHOEDOX_PUBLIC_URL)")
	}
	pass, err := s.newRenderPass(a.User.ID, []string{p.ID}, 2*time.Minute)
	if err != nil {
		return nil, err
	}
	o.Headers = map[string]string{"Authorization": "Bearer " + pass}
	if o.Ready == "" {
		o.Ready = renderReady
	}
	if pdf {
		return r.PDF(ctx, target+path, o)
	}
	return r.Page(ctx, target+path, o)
}

// viewportFor sizes a viewport for a w×h area: the long side 1568 px (what
// AI models look at without scaling down), at least 320 px on the short one.
func viewportFor(w, h float64) (int, int) {
	if w <= 0 || h <= 0 {
		return 1568, 1000
	}
	const long = 1568.0
	if w >= h {
		return int(long), int(math.Max(320, math.Round(long*h/w)))
	}
	return int(math.Max(320, math.Round(long*w/h))), int(long)
}

// BoardExactPNG draws a whiteboard (or one frame by id or title) with the
// real editor in the server-side browser.
func (s *Service) BoardExactPNG(ctx context.Context, a Actor, ref, docRef, frame string, dark bool) ([]byte, error) {
	if s.renderer(ctx, RenderBoards) == nil {
		return nil, ErrRendererOff
	}
	p, doc, wb, err := s.whiteboardDoc(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	// The area decides the shape of the picture.
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	found := frame == ""
	for _, e := range wb.Elements {
		if frame != "" && e.ID != frame && !(e.Type == "frame" && e.Text == frame) {
			continue
		}
		found = true
		a0, b0, a1, b1 := elementBox(e)
		x0, y0, x1, y1 = math.Min(x0, a0), math.Min(y0, b0), math.Max(x1, a1), math.Max(y1, b1)
	}
	if !found {
		return nil, fmt.Errorf("%w: frame %q", ErrNotFound, frame)
	}
	w, h := viewportFor(x1-x0+80, y1-y0+80)
	q := url.Values{"frame": {frame}}
	if dark {
		q.Set("theme", "dark")
	}
	return s.renderPage(ctx, a, RenderBoards, p, "/render/wb/"+url.PathEscape(p.Slug)+"/"+url.PathEscape(doc.ID)+"?"+q.Encode(),
		render.PageOptions{W: w, H: h, Dark: dark}, false)
}

// AppExactPNG takes a picture of an app as it runs, in the server-side
// browser, and keeps it as the app's preview (what app_screenshot shows).
func (s *Service) AppExactPNG(ctx context.Context, a Actor, ref, docRef string, dark bool) ([]byte, error) {
	if s.renderer(ctx, RenderApps) == nil {
		return nil, ErrRendererOff
	}
	p, doc, err := s.docAccess(ctx, a, ref, docRef, store.RoleReader)
	if err != nil {
		return nil, err
	}
	if doc.Type != store.DocApp {
		return nil, invalid("validation.not_app", nil, "%s is not an app", docRef)
	}
	q := url.Values{}
	if dark {
		q.Set("theme", "dark")
	}
	return s.renderPage(ctx, a, RenderApps, p, "/render/app/"+url.PathEscape(p.Slug)+"/"+url.PathEscape(doc.ID)+"?"+q.Encode(),
		render.PageOptions{W: 1280, H: 800, Dark: dark}, false)
}

// PrintPDF prints pages of a project (doc: one page; category: a category;
// both empty: the whole project) as the print view shows them.
func (s *Service) PrintPDF(ctx context.Context, a Actor, ref, doc, category, lang string) ([]byte, string, error) {
	if s.renderer(ctx, RenderPDF) == nil {
		return nil, "", ErrRendererOff
	}
	p, err := s.project(ctx, a, ref, store.RoleReader)
	if err != nil {
		return nil, "", err
	}
	name := p.Name
	q := url.Values{"render": {"1"}}
	if doc != "" {
		_, d, err := s.docAccess(ctx, a, ref, doc, store.RoleReader)
		if err != nil {
			return nil, "", err
		}
		q.Set("doc", d.Slug)
		name = d.Title
	}
	if category != "" {
		q.Set("category", category)
		name = p.Name + " – " + category
	}
	if lang != "" {
		q.Set("lang", lang)
	}
	pdf, err := s.renderPage(ctx, a, RenderPDF, p, "/print/"+url.PathEscape(p.Slug)+"?"+q.Encode(), render.PageOptions{W: 1000, H: 1400}, true)
	return pdf, name, err
}

// RendererUses tells signed-in people which uses of the server-side browser
// are on (the print view offers the server PDF then).
func (s *Service) RendererUses(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	for _, u := range renderUses {
		out[u] = s.renderer(ctx, u) != nil
	}
	return out
}
