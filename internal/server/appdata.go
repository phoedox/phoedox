package server

import (
	"encoding/json"
	"html"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/core"
)

// Apps like artifacts: who is viewing, collections, files people add,
// device consent, apps that save themselves, assets, copies, previews.

func appArgs(r *http.Request) (string, string) { return chi.URLParam(r, "p"), chi.URLParam(r, "doc") }

func (a *api) appMe(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	v, err := a.core.AppMe(r.Context(), actorOf(r), p, d)
	respond(w, v, err)
}

// appEvents streams changes of an app's records as server-sent events.
func (a *api) appEvents(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	ch, stop, err := a.core.WatchApp(r.Context(), actorOf(r), p, d)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer stop()
	streamEvents(w, r, ch)
}

type appQueryIn struct {
	Personal bool `json:"personal,omitempty"`
	core.AppQuery
}

func (a *api) appQuery(w http.ResponseWriter, r *http.Request) {
	var in appQueryIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, d := appArgs(r)
	v, err := a.core.AppQueryRecords(r.Context(), actorOf(r), p, d, chi.URLParam(r, "coll"), in.Personal, in.AppQuery)
	respond(w, v, err)
}

type appRecordIn struct {
	Personal bool            `json:"personal,omitempty"`
	Merge    bool            `json:"merge,omitempty"`
	Data     json.RawMessage `json:"data"`
}

func (a *api) appAddRecord(w http.ResponseWriter, r *http.Request) {
	var in appRecordIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, d := appArgs(r)
	v, err := a.core.AppAddRecord(r.Context(), actorOf(r), p, d, chi.URLParam(r, "coll"), in.Personal, in.Data)
	respondStatus(w, http.StatusCreated, v, err)
}

func (a *api) appPutRecord(w http.ResponseWriter, r *http.Request) {
	var in appRecordIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, d := appArgs(r)
	v, err := a.core.AppPutRecord(r.Context(), actorOf(r), p, d, chi.URLParam(r, "id"), in.Data, in.Merge)
	respond(w, v, err)
}

func (a *api) appDeleteRecord(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	err := a.core.AppDeleteRecord(r.Context(), actorOf(r), p, d, chi.URLParam(r, "id"))
	respond(w, ok{}, err)
}

func (a *api) appUploads(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	v, err := a.core.AppUploads(r.Context(), actorOf(r), p, d)
	respond(w, v, err)
}

func (a *api) appUpload(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	v, err := a.core.AppUpload(r.Context(), actorOf(r), p, d, r.URL.Query().Get("name"), r.Body)
	respondStatus(w, http.StatusCreated, v, err)
}

func (a *api) appUploadContent(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	fc, err := a.core.AppUploadContent(r.Context(), actorOf(r), p, d, chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	a.writeFile(w, r, fc, 0)
}

func (a *api) appDeleteUpload(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	err := a.core.AppDeleteUpload(r.Context(), actorOf(r), p, d, chi.URLParam(r, "id"))
	respond(w, ok{}, err)
}

type appDeviceIn struct {
	Allowed bool `json:"allowed"`
	Forget  bool `json:"forget,omitempty"`
}

func (a *api) appDevice(w http.ResponseWriter, r *http.Request) {
	var in appDeviceIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, d := appArgs(r)
	v, err := a.core.SetAppDevice(r.Context(), actorOf(r), p, d, chi.URLParam(r, "device"), in.Allowed, in.Forget)
	respond(w, v, err)
}

func (a *api) appOwnFiles(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	v, err := a.core.AppOwnFiles(r.Context(), actorOf(r), p, d)
	respond(w, v, err)
}

type appSelfIn struct {
	Files map[string]*string `json:"files"`
}

func (a *api) appSelfSave(w http.ResponseWriter, r *http.Request) {
	var in appSelfIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, d := appArgs(r)
	v, err := a.core.AppSelfSave(r.Context(), actorOf(r), p, d, in.Files)
	respond(w, v, err)
}

func (a *api) appAssets(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	v, err := a.core.AppAssets(r.Context(), actorOf(r), p, d)
	respond(w, v, err)
}

func (a *api) addAppAsset(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	v, err := a.core.AddAppAsset(r.Context(), actorOf(r), p, d, r.URL.Query().Get("name"), r.Body)
	respondStatus(w, http.StatusCreated, v, err)
}

func (a *api) deleteAppAsset(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	err := a.core.DeleteAppAsset(r.Context(), actorOf(r), p, d, chi.URLParam(r, "name"))
	respond(w, ok{}, err)
}

type appAssetsCopyIn struct {
	Project  string   `json:"project,omitempty"`
	Document string   `json:"document"`
	Names    []string `json:"names,omitempty"`
}

func (a *api) copyAppAssets(w http.ResponseWriter, r *http.Request) {
	var in appAssetsCopyIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, d := appArgs(r)
	v, err := a.core.CopyAppAssets(r.Context(), actorOf(r), p, d, in.Project, in.Document, in.Names)
	respond(w, v, err)
}

func (a *api) setAppPreview(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 3<<20))
	if err != nil {
		writeErr(w, err)
		return
	}
	p, d := appArgs(r)
	err = a.core.SetAppPreview(r.Context(), actorOf(r), p, d, body)
	respond(w, ok{}, err)
}

func (a *api) appPreview(w http.ResponseWriter, r *http.Request) {
	p, d := appArgs(r)
	fc, err := a.core.AppPreview(r.Context(), actorOf(r), p, d)
	if err != nil {
		writeErr(w, err)
		return
	}
	a.writeFile(w, r, fc, 0)
}

func (a *api) appGallery(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.AppGallery(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

type sharedAppCopyIn struct {
	Project string `json:"project"`
	Title   string `json:"title,omitempty"`
}

func (a *api) copySharedApp(w http.ResponseWriter, r *http.Request) {
	var in sharedAppCopyIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.CopySharedApp(r.Context(), actorOf(r), chi.URLParam(r, "token"), in.Project, in.Title)
	respondStatus(w, http.StatusCreated, v, err)
}

func (a *api) sharedAppPreview(w http.ResponseWriter, r *http.Request) {
	fc, err := a.core.SharedAppPreview(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer fc.Reader.Close()
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "public, max-age=600")
	http.ServeContent(w, r, "", fc.File.CreatedAt.Time, fc.Reader)
}

// sharedAppPage serves the page of a public app with a link preview
// (title and picture for chats and social networks).
func sharedAppPage(web fs.FS, svc *core.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		index, err := fs.ReadFile(web, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		token := chi.URLParam(r, "token")
		page := string(index)
		if m, err := svc.SharedAppInfo(r.Context(), token); err == nil {
			meta := `<meta property="og:type" content="website"><meta property="og:title" content="` + html.EscapeString(m.Title) + `">` +
				`<meta property="og:site_name" content="` + html.EscapeString(m.Project) + `">`
			if m.Preview {
				img := strings.TrimRight(svc.PublicURL, "/") + "/api/v1/shared-apps/" + html.EscapeString(token) + "/preview.png"
				meta += `<meta property="og:image" content="` + img + `"><meta name="twitter:card" content="summary_large_image">`
			}
			page = strings.Replace(page, "</head>", meta+"</head>", 1)
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(page))
	}
}
