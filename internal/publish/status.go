package publish

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

var statusTmpl = template.Must(template.New("status").Funcs(template.FuncMap{
	"t":    func(t texts, k string) string { return t[k] },
	"time": func(v store.Time) string { return v.UTC().Format("02.01.2006 15:04 UTC") },
	"up":   func(t texts, v float64) string { return fmt.Sprintf(t["status_uptime"], fmt.Sprintf("%.2f", v)) },
}).Parse(`<div class="status-head st-{{.P.Overall}}" role="status"><span class="dot"></span>{{t .T (printf "status_%s" .P.Overall)}}</div>
{{if .P.Checks}}<ul class="checks">{{range .P.Checks}}<li><span class="dot {{.Status}}" aria-hidden="true"></span><span class="name">{{.Name}}</span>
<span class="up">{{up $.T .Uptime}}</span><span>{{t $.T (printf "status_%s" .Status)}}</span></li>{{end}}</ul>
{{else}}<p class="note">{{t .T "status_no_checks"}}</p>{{end}}
<h2>{{t .T "status_incidents"}}</h2>
{{range .P.Incidents}}<section class="incident{{if .ResolvedAt.IsZero}} open{{end}}">
<h3>{{.Title}}</h3>
<p class="when">{{time .StartedAt}} – {{if .ResolvedAt.IsZero}}{{t $.T "status_ongoing"}}{{else}}{{time .ResolvedAt}}{{end}}</p>
<ol>{{range .Updates}}<li><b>{{t $.T (printf "incident_%s" .Status)}}</b>{{.Body}}<time>{{time .CreatedAt}}{{if .Auto}} · {{t $.T "status_auto"}}{{end}}</time></li>{{end}}</ol>
</section>{{else}}<p>{{t .T "status_no_incidents"}}</p>{{end}}
<p class="note">{{t .T "status_updated"}}: {{time .P.Updated}}</p>`))

func statusBody(p core.StatusPage, t texts) template.HTML {
	var b bytes.Buffer
	if err := statusTmpl.Execute(&b, map[string]any{"P": p, "T": t}); err != nil {
		return template.HTML(template.HTMLEscapeString(err.Error()))
	}
	return template.HTML(b.String())
}

// StatusPage renders a project's public status page (/status/<token>).
// base is the page's own address; its styles load below it (see Asset).
func (s *Site) StatusPage(w http.ResponseWriter, r *http.Request, p core.StatusPage, base string) {
	lang := uiLocale(r)
	t := textsFor(lang)
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'")
	h.Set("Cache-Control", "public, max-age=30")
	h.Set("X-Robots-Tag", "noindex")
	pub := store.Publication{Title: p.Title, Access: "public", NoIndex: true}
	s.render(w, r, http.StatusOK, &page{T: t, Base: base, Pub: pub, Title: p.Title + " – " + t["status_title"], Body: statusBody(p, t), Plain: true, Lang: lang})
}

// pubStatus is /status of a publication: public checks and incidents of its projects.
func (s *Site) pubStatusPage(w http.ResponseWriter, r *http.Request, p store.Publication, base string, site []core.PubProject, t texts, embed bool) {
	ids := make([]string, 0, len(site))
	for _, pp := range site {
		ids = append(ids, pp.Project.ID)
	}
	st, err := s.Core.PublicStatus(r.Context(), p.Title, ids, time.Now())
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	s.render(w, r, http.StatusOK, &page{T: t, Base: base, Pub: p, Site: site, Title: t["status_title"] + " – " + p.Title, Body: statusBody(st, t), Plain: true, Embed: embed, Source: base + "/status"})
}

// Asset serves the styles and scripts of rendered pages below another base
// (status pages); rel is e.g. "/pub/pub-abc.css".
func (s *Site) Asset(w http.ResponseWriter, r *http.Request, rel string) bool {
	if !strings.HasPrefix(rel, "/pub/") && !strings.HasPrefix(rel, "/assets/") {
		return false
	}
	s.asset(w, r, strings.TrimPrefix(rel, "/"))
	return true
}
