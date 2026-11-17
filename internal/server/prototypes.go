package server

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

var fileNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// prototypeData reads the data sources of a whiteboard prototype with the
// viewer's rights (live sources; sample rows where they cannot be read).
func (a *api) prototypeData(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.PrototypeData(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), chi.URLParam(r, "id"))
	respond(w, v, err)
}

// prototypeExport returns one HTML file that plays a prototype on its own
// (?as=json: {html, name} for the app's player).
func (a *api) prototypeExport(w http.ResponseWriter, r *http.Request) {
	page, name, err := a.core.PrototypeHTML(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), chi.URLParam(r, "id"), r.URL.Query().Get("lang"), core.MaxExportImages)
	if err != nil {
		writeErr(w, err)
		return
	}
	if r.URL.Query().Get("as") == "json" {
		writeJSON(w, http.StatusOK, map[string]string{"html": string(page), "name": name})
		return
	}
	file := strings.Trim(fileNameRe.ReplaceAllString(name, "-"), "-")
	if file == "" {
		file = "prototype"
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+file+`.html"`)
	// Never run on the app's origin, even if opened in place.
	h.Set("Content-Security-Policy", "sandbox allow-scripts; default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data:")
	h.Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(page)
}

type prototypeAppIn struct {
	Lang string `json:"lang,omitempty"`
}

// prototypeApp creates an app document from a prototype.
func (a *api) prototypeApp(w http.ResponseWriter, r *http.Request) {
	var in prototypeAppIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.PrototypeToApp(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), chi.URLParam(r, "id"), in.Lang)
	respond(w, v, err)
}

// whiteboardComment pins a comment to a screen while playing a prototype.
func (a *api) whiteboardComment(w http.ResponseWriter, r *http.Request) {
	var in core.PlayComment
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.CommentWhiteboard(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
	respond(w, v, err)
}
