package server

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/auth"
	"github.com/phoedox/phoedox/internal/config"
	"github.com/phoedox/phoedox/internal/core"
	"github.com/phoedox/phoedox/internal/store"
)

type api struct {
	core    *core.Service
	auth    *auth.Auth
	cfg     config.Config
	limiter *limiter
	// regLimiter limits open OAuth client registrations per IP.
	regLimiter *limiter
	// runLimiter limits request block runs per user.
	runLimiter *limiter
}

func clientIP(r *http.Request) string {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

type ctxKey struct{}

func actorOf(r *http.Request) core.Actor { return r.Context().Value(ctxKey{}).(core.Actor) }

func (a *api) routes(r chi.Router) {
	r.Post("/auth/login", a.login)
	r.Post("/auth/logout", a.logout)
	r.Get("/instance", func(w http.ResponseWriter, _ *http.Request) {
		info := map[string]any{"version": Version, "domain": a.cfg.Domain, "passwordReset": a.resetAvailable()}
		if a.auth.OIDC != nil {
			info["oidc"] = map[string]string{"name": a.auth.OIDC.Config.Name}
		}
		writeJSON(w, http.StatusOK, info)
	})
	r.Get("/auth/oidc/start", a.oidcStart)
	r.Get("/auth/oidc/callback", a.oidcCallback)
	r.Group(func(r chi.Router) {
		r.Use(requireHeader)
		r.Post("/auth/reset-request", a.resetRequest)
		r.Get("/auth/links/{token}", a.linkInfo)
		r.Post("/auth/links/{token}", a.completeLink)
		r.Post("/auth/login/totp", a.loginTOTP)
		r.Post("/auth/login/recovery", a.loginRecovery)
		r.Post("/auth/login/passkey/begin", a.loginPasskeyBegin)
		r.Post("/auth/login/passkey/finish", a.loginPasskeyFinish)
		r.Post("/auth/passkey/begin", a.passkeyBegin)
		r.Post("/auth/passkey/finish", a.passkeyFinish)
	})

	r.Group(func(r chi.Router) {
		r.Use(a.requireActor)
		r.Get("/me", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, actorOf(r).User) })
		// Account changes need the full rights of the user (no narrowed token).
		r.Group(func(r chi.Router) {
			r.Use(requireFull)
			r.Patch("/me", a.updateProfile)
			r.Post("/me/password", a.changePassword)
			r.Get("/me/security", a.security)
			r.Post("/me/totp", a.totpSetup)
			r.Post("/me/totp/confirm", a.totpConfirm)
			r.Post("/me/totp/disable", a.totpDisable)
			r.Post("/me/recovery-codes", a.recoveryCodes)
			r.Post("/me/passkeys/begin", a.passkeyRegisterBegin)
			r.Post("/me/passkeys/finish", a.passkeyRegisterFinish)
			r.Patch("/me/passkeys/{id}", a.passkeyRename)
			r.Delete("/me/passkeys/{id}", a.passkeyDelete)
			r.Get("/oauth/request", a.oauthRequest)
			r.Post("/oauth/authorize", a.oauthAuthorize)
			r.Delete("/oauth/grants/{id}", a.revokeGrant)
		})
		r.Get("/oauth/grants", a.listGrants)
		r.Route("/admin", func(r chi.Router) {
			r.Use(requireFull)
			r.Get("/settings", a.getSettings)
			r.Put("/settings", a.putSettings)
			r.Get("/users", a.listUsers)
			r.Post("/users", a.inviteUser)
			r.Patch("/users/{id}", a.updateUser)
			r.Post("/users/{id}/link", a.userLink)
			r.Post("/users/{id}/reset-2fa", a.reset2FA)
		})
		r.Get("/tokens", a.listTokens)
		r.With(requireFull).Post("/tokens", a.createToken)
		r.With(requireFull).Delete("/tokens/{id}", a.revokeToken)

		r.Get("/search", a.search)
		r.Get("/projects", a.listProjects)
		r.Post("/projects", a.createProject)
		r.Route("/projects/{p}", func(r chi.Router) {
			r.Get("/", a.getProject)
			r.Patch("/", a.updateProject)
			r.Get("/members", a.listMembers)
			r.Put("/members", a.setMember)
			r.Get("/documents", a.listDocuments)
			r.Post("/documents", a.createDocument)
			r.Get("/documents/{d}", a.getDocument)
			r.Patch("/documents/{d}", a.updateDocument)
			r.Delete("/documents/{d}", a.deleteDocument)
			r.Get("/documents/{d}/versions", a.listVersions)
			r.Put("/documents/{d}/live", a.liveSave)
			r.Post("/documents/{d}/requests/{n}/run", a.runRequest)
			r.Get("/env", a.listEnv)
			r.Put("/env/{name}", a.setEnv)
			r.Delete("/env/{name}", a.deleteEnv)
			r.Get("/documents/{d}/versions/{n}", a.getVersion)
			r.Post("/documents/{d}/versions/{n}/restore", a.restoreVersion)
			r.Get("/trash", a.listTrash)
			r.Post("/trash/{id}/restore", a.restoreTrash)
			r.Delete("/trash/{id}", a.purgeTrash)
			r.Get("/favorites", a.listFavorites)
			r.Put("/favorites", a.reorderFavorites)
			r.Put("/favorites/{d}", a.setFavorite(true))
			r.Delete("/favorites/{d}", a.setFavorite(false))
			r.Get("/decisions", a.listDecisions)
			r.Get("/model", a.dataModel)
			r.Put("/kinds/{k}", a.defineKind)
			r.Delete("/kinds/{k}", a.deleteKind)
			r.Get("/graph", a.graph)
			r.Get("/entities", a.listEntities)
			r.Post("/entities", a.upsertEntity)
			r.Get("/entities/{e}", a.getEntity)
			r.Delete("/entities/{e}", a.deleteEntity)
			r.Get("/entities/{e}/traverse", a.traverse)
			r.Post("/edges", a.link)
			r.Post("/edges/remove", a.unlink)
			r.Get("/suggestions", a.listSuggestions)
		})
		r.Post("/suggestions/{id}/accept", a.decide(true))
		r.Post("/suggestions/{id}/reject", a.decide(false))
	})
}

// requireActor authenticates via Bearer token or session cookie. Cookie
// requests that change data must send X-Phoedox: 1 (CSRF protection).
func (a *api) requireActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, viaCookie, err := a.auth.FromRequest(r, "token")
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, errBody("unauthorized", "Please sign in."))
			return
		}
		if viaCookie && r.Method != http.MethodGet && r.Header.Get("X-Phoedox") != "1" {
			writeJSON(w, http.StatusForbidden, errBody("csrf", "Missing X-Phoedox header."))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, actor)))
	})
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 5<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &core.InvalidError{Key: "validation.json", Msg: "invalid JSON: " + err.Error()}
	}
	return nil
}

func (a *api) login(w http.ResponseWriter, r *http.Request) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if !a.limiter.allow(ip) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many attempts. Please wait a few minutes."))
		return
	}
	var in struct{ Email, Password string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	step, err := a.auth.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	if step.Cookie == nil {
		writeJSON(w, http.StatusOK, step) // {challenge, methods}: second factor needed
		return
	}
	http.SetCookie(w, step.Cookie)
	writeJSON(w, http.StatusOK, step.User)
}

func (a *api) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, a.auth.Logout(r))
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) listTokens(w http.ResponseWriter, r *http.Request) {
	ts, err := a.auth.DB.ListTokens(r.Context(), actorOf(r).User.ID)
	respond(w, ts, err)
}

func (a *api) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string   `json:"name"`
		Access   string   `json:"access"`
		Projects []string `json:"projects"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	access, projects, err := a.core.TokenScope(r.Context(), actorOf(r), in.Access, in.Projects)
	if err != nil {
		writeErr(w, err)
		return
	}
	t, plain, err := a.auth.CreateToken(r.Context(), actorOf(r).User.ID, in.Name, access, projects)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"token": t, "secret": plain})
}

func (a *api) revokeToken(w http.ResponseWriter, r *http.Request) {
	err := a.auth.DB.RevokeToken(r.Context(), actorOf(r).User.ID, chi.URLParam(r, "id"))
	respond(w, nil, err)
}

func (a *api) search(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	hits, err := a.core.Search(r.Context(), actorOf(r), r.URL.Query().Get("q"), r.URL.Query().Get("project"), limit)
	respond(w, hits, err)
}

func (a *api) listProjects(w http.ResponseWriter, r *http.Request) {
	ps, err := a.core.Projects(r.Context(), actorOf(r))
	respond(w, ps, err)
}

func (a *api) createProject(w http.ResponseWriter, r *http.Request) {
	var in core.ProjectInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, err := a.core.CreateProject(r.Context(), actorOf(r), in)
	respondStatus(w, http.StatusCreated, p, err)
}

func (a *api) getProject(w http.ResponseWriter, r *http.Request) {
	p, err := a.core.Project(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, p, err)
}

func (a *api) updateProject(w http.ResponseWriter, r *http.Request) {
	var in core.ProjectInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, err := a.core.UpdateProject(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, p, err)
}

func (a *api) listMembers(w http.ResponseWriter, r *http.Request) {
	ms, err := a.core.Members(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, ms, err)
}

func (a *api) setMember(w http.ResponseWriter, r *http.Request) {
	var in struct{ Email, Role string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, nil, a.core.SetMember(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Email, in.Role))
}

func (a *api) listDocuments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ds, err := a.core.Documents(r.Context(), actorOf(r), chi.URLParam(r, "p"), store.DocFilter{Type: q.Get("type"), Category: q.Get("category")})
	respond(w, ds, err)
}

func (a *api) listDecisions(w http.ResponseWriter, r *http.Request) {
	ds, err := a.core.Decisions(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("status"))
	respond(w, ds, err)
}

func (a *api) createDocument(w http.ResponseWriter, r *http.Request) {
	var in core.DocumentInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.CreateDocument(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respondStatus(w, http.StatusCreated, res, err)
}

func (a *api) getDocument(w http.ResponseWriter, r *http.Request) {
	d, err := a.core.Document(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, d, err)
}

func (a *api) updateDocument(w http.ResponseWriter, r *http.Request) {
	var in core.DocumentPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.UpdateDocument(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
	respond(w, res, err)
}

func (a *api) deleteDocument(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.DeleteDocument(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, res, err)
}

func (a *api) listVersions(w http.ResponseWriter, r *http.Request) {
	vs, err := a.core.VersionHistory(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, vs, err)
}

func (a *api) dataModel(w http.ResponseWriter, r *http.Request) {
	m, err := a.core.DataModel(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, m, err)
}

func (a *api) defineKind(w http.ResponseWriter, r *http.Request) {
	var in core.KindInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	in.ID = chi.URLParam(r, "k")
	res, err := a.core.DefineKind(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, res, err)
}

func (a *api) deleteKind(w http.ResponseWriter, r *http.Request) {
	err := a.core.DeleteKind(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "k"))
	respond(w, map[string]bool{"ok": err == nil}, err)
}

func (a *api) graph(w http.ResponseWriter, r *http.Request) {
	g, err := a.core.ProjectGraph(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, g, err)
}

func (a *api) listEntities(w http.ResponseWriter, r *http.Request) {
	es, err := a.core.Entities(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("kind"))
	respond(w, es, err)
}

func (a *api) upsertEntity(w http.ResponseWriter, r *http.Request) {
	var in core.EntityInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.UpsertEntity(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, res, err)
}

func (a *api) getEntity(w http.ResponseWriter, r *http.Request) {
	e, err := a.core.Entity(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"))
	respond(w, e, err)
}

func (a *api) deleteEntity(w http.ResponseWriter, r *http.Request) {
	res, err := a.core.DeleteEntity(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"))
	respond(w, res, err)
}

func (a *api) traverse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	depth, _ := strconv.Atoi(q.Get("depth"))
	hops, err := a.core.Traverse(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"), q.Get("rel"), q.Get("dir"), depth)
	respond(w, hops, err)
}

func (a *api) link(w http.ResponseWriter, r *http.Request) {
	var in core.EdgeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.Link(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, res, err)
}

func (a *api) unlink(w http.ResponseWriter, r *http.Request) {
	var in core.EdgeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.Unlink(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, res, err)
}

func (a *api) listSuggestions(w http.ResponseWriter, r *http.Request) {
	ss, err := a.core.Suggestions(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("status"))
	respond(w, ss, err)
}

func (a *api) decide(accept bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res, err := a.core.DecideSuggestion(r.Context(), actorOf(r), chi.URLParam(r, "id"), accept)
		respond(w, res, err)
	}
}

func respond(w http.ResponseWriter, v any, err error) { respondStatus(w, http.StatusOK, v, err) }

func respondStatus(w http.ResponseWriter, status int, v any, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	if v == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, status, v)
}

// limiter allows n attempts per key within a window (login protection).
type limiter struct {
	mu     sync.Mutex
	n      int
	window time.Duration
	hits   map[string][]time.Time
}

func newLimiter(n int, window time.Duration) *limiter {
	return &limiter{n: n, window: window, hits: map[string][]time.Time{}}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var recent []time.Time
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			recent = append(recent, t)
		}
	}
	if len(recent) >= l.n {
		l.hits[key] = recent
		return false
	}
	l.hits[key] = append(recent, now)
	return true
}
