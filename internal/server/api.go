package server

import (
	"context"
	"encoding/json"
	"github.com/phoedox/phoedox/internal/backup"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
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
	// root is the whole router (for the OpenAPI description).
	root chi.Routes
	// backups is nil in the hosted mode.
	backups *backup.Manager
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
	r.Get("/status/{token}", a.publicStatus)
	// One-time secret links: POST so link previews cannot burn them.
	r.Post("/secret-links/{id}/open", a.openSecretLink)
	r.Get("/instance", func(w http.ResponseWriter, req *http.Request) {
		info := map[string]any{"version": Version, "domain": a.cfg.Domain, "passwordReset": a.resetAvailable()}
		// On an organization's own app domain the app shows its name.
		if c := a.core.AppContextFor(req.Context(), req.Host); c.Org != nil {
			info["org"] = c.Org
		}
		if a.auth.OIDC != nil {
			info["oidc"] = map[string]string{"name": a.auth.OIDC.Config.Name}
		}
		writeJSON(w, http.StatusOK, info)
	})
	r.Get("/openapi.json", a.serveOpenAPI)
	r.Get("/shared/{token}", a.sharedView)
	r.Get("/shared/{token}/documents/{d}", a.sharedDocument)
	// Agents (ADR-0033): their own token, not a user session.
	r.Post("/agent/enroll", a.agentEnroll)
	r.Post("/agent/report", a.agentReport)
	r.Get("/agent/jobs", a.agentJobs)
	r.Post("/agent/jobs/{id}/result", a.agentJobResult)
	r.Get("/agent/releases/{tag}/{asset}", a.agentRelease)
	r.Get("/shared/{token}/files/{id}", a.sharedFile)
	r.Get("/shared/{token}/files/{id}/{name}", a.sharedFile)
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
		r.Get("/me/notifications", a.listNotifications)
		r.Get("/me/tasks", a.myTasks)
		r.Post("/me/notifications/read", a.readNotifications)
		// Account changes need the full rights of the user (no narrowed token).
		r.Group(func(r chi.Router) {
			r.Use(requireFull)
			r.Patch("/me", a.updateProfile)
			r.Put("/me/avatar", a.setAvatar)
			r.Get("/publications", a.listPublications)
			r.Post("/publications", a.createPublication)
			r.Patch("/publications/{id}", a.updatePublication)
			r.Delete("/publications/{id}", a.deletePublication)
			r.Get("/publications/{id}/releases", a.listReleases)
			r.Post("/publications/{id}/releases", a.createRelease)
			r.Delete("/me/avatar", a.deleteAvatar)
			r.Get("/images/user/{id}", a.userImage)
			r.Post("/me/password", a.changePassword)
			r.Get("/me/security", a.security)
			r.Get("/me/notification-settings", a.notificationSettings)
			r.Put("/me/notification-settings", a.saveNotificationSettings)
			r.Post("/secret-links", a.createSecretLink)
			r.Get("/me/recent", a.recent)
			r.Post("/me/recent", a.recordView)
			r.Delete("/me/recent", a.clearRecent)
			r.Get("/me/shortcuts", a.shortcuts)
			r.Put("/me/shortcuts", a.saveShortcuts)
			r.Post("/me/push-subscriptions", a.addPushSubscription)
			r.Post("/me/push-subscriptions/remove", a.deletePushSubscription)
			r.Post("/me/notifications/test", a.testNotification)
			r.Get("/me/signing-keys", a.signingKeys)
			r.Post("/me/signing-keys", a.addSigningKey)
			r.Delete("/me/signing-keys/{id}", a.deleteSigningKey)
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
			r.Get("/backups", a.listBackups)
			r.Post("/backups", a.createBackup)
			r.Post("/backups/upload", a.uploadBackup)
			r.Get("/backups/{name}", a.downloadBackup)
			r.Delete("/backups/{name}", a.deleteBackup)
			r.Post("/backups/{name}/restore", a.restoreBackup)
			r.Get("/settings", a.getSettings)
			r.Put("/settings", a.putSettings)
			r.Get("/users", a.listUsers)
			r.Post("/users", a.inviteUser)
			r.Patch("/users/{id}", a.updateUser)
			r.Post("/users/{id}/link", a.userLink)
			r.Post("/users/{id}/reset-2fa", a.reset2FA)
			r.Get("/events", a.adminEvents)
			r.Get("/audit/verify", a.verifyAudit)
			r.Get("/plugins", a.listPlugins)
			r.Post("/plugins", a.uploadPlugin)
			r.Get("/plugins/catalog", a.pluginCatalog)
			r.Get("/plugins/collections", a.pluginCollections)
			r.Post("/plugins/install", a.installFromStore)
			r.Patch("/plugins/{id}", a.patchPlugin)
			r.Delete("/plugins/{id}", a.deletePlugin)
			r.Get("/plugins/{id}/logs", a.pluginLogs)
			r.Put("/plugins/{id}/license", a.pluginLicense)
			r.Post("/plugins/{id}/license/redeem", a.redeemLicense)
		})
		r.Get("/plugins/{id}/{v}/files/*", a.pluginFile)
		r.With(requireFull).Route("/orgs", func(r chi.Router) {
			r.Get("/", a.listOrgs)
			r.Post("/", a.createOrg)
			r.Get("/{o}", a.getOrg)
			r.Patch("/{o}", a.updateOrg)
			r.Put("/{o}/members", a.setOrgMember)
			r.Get("/{o}/domain", a.orgDomain)
			r.Put("/{o}/domain", a.setOrgDomain)
			r.Post("/{o}/domain/verify", a.verifyOrgDomain)
			r.Delete("/{o}/members/{user}", a.removeOrgMember)
			r.Post("/{o}/groups", a.saveGroup)
			r.Patch("/{o}/groups/{g}", a.saveGroup)
			r.Delete("/{o}/groups/{g}", a.deleteGroup)
			r.Post("/{o}/groups/{g}/members", a.groupMember(true))
			r.Post("/{o}/groups/{g}/members/remove", a.groupMember(false))
		})
		r.Head("/uploads/{id}", a.tusHead)
		r.Patch("/uploads/{id}", a.tusPatch)
		r.Delete("/uploads/{id}", a.tusDelete)
		r.Get("/files/{id}", a.serveFile)
		r.Get("/files/{id}/{name}", a.serveFile)
		r.Get("/tokens", a.listTokens)
		r.With(requireFull).Post("/tokens", a.createToken)
		r.With(requireFull).Delete("/tokens/{id}", a.revokeToken)

		r.Get("/search", a.search)
		r.Get("/projects", a.listProjects)
		r.Get("/templates", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, core.Templates(r.URL.Query().Get("lang")))
		})
		r.Post("/projects", a.createProject)
		r.Post("/projects/import", a.importProject)
		r.Post("/projects/import-remote", a.importRemote)
		r.Post("/projects/import-remote/list", a.remoteProjects)
		r.Route("/projects/{p}", func(r chi.Router) {
			r.Get("/", a.getProject)
			r.Patch("/", a.updateProject)
			r.Get("/members", a.listMembers)
			r.Put("/members", a.setMember)
			r.Delete("/members/{user}", a.removeMember)
			r.Get("/groups", a.listProjectGroups)
			r.Put("/groups/{g}", a.setProjectGroup)
			r.Delete("/groups/{g}", a.setProjectGroup)
			r.Get("/documents/{d}/sharing", a.sharing)
			r.Put("/documents/{d}/sharing", a.setRestricted)
			r.Post("/documents/{d}/sharing/grants", a.shareDocument)
			r.Delete("/documents/{d}/sharing/grants/{id}", a.unshareDocument)
			r.Get("/webhooks", a.listWebhooks)
			r.Post("/webhooks", a.createWebhook)
			r.Patch("/webhooks/{id}", a.updateWebhook)
			r.Delete("/webhooks/{id}", a.deleteWebhook)
			r.Get("/webhooks/{id}/deliveries", a.webhookDeliveries)
			r.Post("/webhooks/{id}/test", a.testWebhook)
			r.Get("/share-links", a.listShareLinks)
			r.Post("/share-links", a.createShareLink)
			r.Delete("/share-links/{id}", a.revokeShareLink)
			r.Get("/documents", a.listDocuments)
			r.Post("/documents", a.createDocument)
			r.Get("/documents/{d}", a.getDocument)
			r.Patch("/documents/{d}", a.updateDocument)
			r.Delete("/documents/{d}", a.deleteDocument)
			r.Get("/documents/{d}/versions", a.listVersions)
			r.Get("/documents/{d}/translations/{lang}", a.getTranslation)
			r.Put("/documents/{d}/translations/{lang}", a.saveTranslation)
			r.Delete("/documents/{d}/translations/{lang}", a.deleteTranslation)
			r.Get("/drafts", a.projectDrafts)
			r.Get("/documents/{d}/draft", a.getDraft)
			r.Post("/documents/{d}/draft", a.startDraft)
			r.Put("/documents/{d}/draft", a.saveDraft)
			r.Delete("/documents/{d}/draft", a.cancelDraft)
			r.Post("/documents/{d}/draft/publish", a.publishDraft)
			r.Post("/documents/{d}/draft/review", a.requestReview)
			r.Post("/documents/{d}/draft/reject", a.rejectReview)
			r.Get("/documents/{d}/rules", a.pageRules)
			r.Put("/documents/{d}/rules", a.setPageRules)
			r.Post("/documents/{d}/reviewed", a.markReviewed)
			r.Post("/documents/{d}/read", a.confirmRead)
			r.Get("/approval", a.getApproval)
			r.Put("/approval", a.setApproval)
			r.Get("/maintenance", a.listMaintenance)
			r.Post("/maintenance", a.saveMaintenance)
			r.Put("/maintenance/{id}", a.saveMaintenance)
			r.Delete("/maintenance/{id}", a.deleteMaintenance)
			r.Post("/documents/{d}/requests/{n}/run", a.runRequest)
			r.Get("/documents/{d}/comments", a.listComments)
			r.Post("/documents/{d}/comments", a.addComment)
			r.Patch("/comments/{id}", a.updateComment)
			r.Delete("/comments/{id}", a.deleteComment)
			r.Get("/comments/open", a.openComments)
			r.Get("/export", a.exportProject)
			r.Get("/git", a.projectGit)
			r.Put("/git", a.saveProjectGit)
			r.Post("/git/sync", a.syncProjectGit)
			r.Get("/checks", a.listChecks)
			r.Get("/status", a.statuses)
			r.Post("/checks", a.createCheck)
			r.Patch("/checks/{id}", a.updateCheck)
			r.Delete("/checks/{id}", a.deleteCheck)
			r.Post("/checks/{id}/run", a.runCheck)
			r.Get("/gaps", a.gaps)
			r.Get("/changes", a.changes)
			r.Post("/documents/{d}/archive", a.archiveLink)
			r.Post("/links/archive", a.archiveAllLinks)
			r.Get("/incidents", a.listIncidents)
			r.Post("/incidents", a.createIncident)
			r.Patch("/incidents/{id}", a.updateIncident)
			r.Delete("/incidents/{id}", a.deleteIncident)
			r.Post("/incidents/{id}/updates", a.addIncidentUpdate)
			r.Get("/status-page", a.projectStatusPage)
			r.Put("/status-page", a.setProjectStatusPage)
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
			r.Get("/agents", a.listAgents)
			r.Post("/agents/enrollments", a.createEnrollment)
			r.Delete("/agents/{id}", a.revokeAgent)
			r.Get("/image", a.projectImage)
			r.Put("/image", a.setProjectImage)
			r.Delete("/image", a.deleteProjectImage)
			r.Get("/tasks", a.listTasks)
			r.Get("/boards/{d}/events", a.boardEvents)
			r.Post("/boards/{d}/tasks", a.createTask)
			r.Patch("/tasks/{id}", a.updateTask)
			r.Delete("/tasks/{id}", a.deleteTask)
			r.Get("/files", a.listFiles)
			r.Post("/files", a.uploadFile)
			r.Patch("/files/{id}", a.renameFile)
			r.Get("/files/{id}/versions", a.fileVersions)
			r.Post("/files/{id}/versions", a.newFileVersion)
			r.Post("/files/{id}/plan-image", a.planImage)
			r.Get("/files/{id}/text", a.fileText)
			r.Post("/files/{id}/versions/{n}/restore", a.restoreFileVersion)
			r.Get("/storage", a.storage)
			r.Put("/storage", a.setStorage)
			r.Options("/uploads", a.tusOptions)
			r.Post("/uploads", a.tusCreate)
			r.Delete("/files/{id}", a.deleteFile)
			r.Get("/model", a.dataModel)
			r.Put("/kinds/{k}", a.defineKind)
			r.Delete("/kinds/{k}", a.deleteKind)
			r.Get("/graph", a.graph)
			r.Get("/entities", a.listEntities)
			r.Post("/entities", a.upsertEntity)
			r.Get("/entities/{e}", a.getEntity)
			r.Delete("/entities/{e}", a.deleteEntity)
			r.Get("/entities/{e}/traverse", a.traverse)
			r.Get("/entities/{e}/diagnose", a.diagnose)
			r.Get("/entities/{e}/tree", a.softwareTree)
			r.Get("/events", a.projectEvents)
			r.Get("/topology", a.topology)
			r.Get("/duplicates", a.duplicates)
			r.Post("/import/markdown", a.importMarkdown)
			r.Post("/import/openapi", a.importOpenAPI)
			r.Post("/entities-merge", a.mergeEntities)
			r.Get("/entities/{e}/agent-actions", a.entityAgentActions)
			r.Post("/entities/{e}/agent-jobs", a.createAgentJob)
			r.Post("/agent-jobs/{id}/sign", a.signAgentJob)
			r.Post("/edges", a.link)
			r.Post("/edges/remove", a.unlink)
			r.Get("/suggestions", a.listSuggestions)
			r.Get("/plugins", a.projectPlugins)
			r.Put("/plugins/{id}", a.setProjectPlugin)
			r.Put("/plugins/{id}/secrets", a.setPluginSecrets)
			r.Post("/plugins/{id}/tools/{tool}", a.pluginTool)
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
	var in struct{ Email, Role, Until string }
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	until, err := parseUntil(in.Until)
	if err != nil {
		writeErr(w, core.Invalid("validation.guest_until", nil, "until must be a date"))
		return
	}
	respond(w, nil, a.core.SetMemberUntil(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Email, in.Role, until))
}

func (a *api) listDocuments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ds, err := a.core.Documents(r.Context(), actorOf(r), chi.URLParam(r, "p"), store.DocFilter{Type: q.Get("type"), Category: q.Get("category"), Locale: q.Get("lang")})
	respond(w, ds, err)
}

func (a *api) listDecisions(w http.ResponseWriter, r *http.Request) {
	ds, err := a.core.Decisions(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("status"), r.URL.Query().Get("q"))
	respond(w, ds, err)
}

// readImage reads an uploaded picture (raw body, limited).
func readImage(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 600<<10))
}

func serveImage(w http.ResponseWriter, img store.Image, err error) {
	if err != nil {
		writeErr(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", img.Mime)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// The URL carries the version (?v=…), so the picture can be cached for long.
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	_, _ = w.Write(img.Data)
}

func (a *api) setAvatar(w http.ResponseWriter, r *http.Request) {
	data, err := readImage(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetAvatar(r.Context(), actorOf(r), data)
	respond(w, map[string]string{"version": v}, err)
}

func (a *api) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteAvatar(r.Context(), actorOf(r)))
}

func (a *api) userImage(w http.ResponseWriter, r *http.Request) {
	img, err := a.core.UserImage(r.Context(), actorOf(r), chi.URLParam(r, "id"))
	serveImage(w, img, err)
}

func (a *api) projectImage(w http.ResponseWriter, r *http.Request) {
	img, err := a.core.ProjectImage(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	serveImage(w, img, err)
}

func (a *api) setProjectImage(w http.ResponseWriter, r *http.Request) {
	data, err := readImage(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetProjectImage(r.Context(), actorOf(r), chi.URLParam(r, "p"), data)
	respond(w, map[string]string{"version": v}, err)
}

func (a *api) deleteProjectImage(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteProjectImage(r.Context(), actorOf(r), chi.URLParam(r, "p")))
}

func (a *api) agentEnroll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code     string `json:"code"`
		Hostname string `json:"hostname"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if !a.limiter.allow("enroll:" + clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many attempts."))
		return
	}
	e, err := a.core.EnrollAgent(r.Context(), in.Code, in.Hostname)
	respondStatus(w, http.StatusCreated, e, err)
}

func (a *api) agentJobs(w http.ResponseWriter, r *http.Request) {
	wait, _ := strconv.Atoi(r.URL.Query().Get("wait"))
	jobs, err := a.core.AgentJobs(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), time.Duration(wait)*time.Second)
	respond(w, map[string]any{"jobs": jobs}, err)
}

func (a *api) agentJobResult(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OK     bool   `json:"ok"`
		Output string `json:"output"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeErr(w, &core.InvalidError{Key: "validation.json", Msg: "invalid JSON: " + err.Error()})
		return
	}
	err := a.core.AgentJobResult(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), chi.URLParam(r, "id"), in.OK, in.Output)
	respond(w, map[string]bool{"ok": true}, err)
}

func (a *api) signingKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := a.core.SigningKeys(r.Context(), actorOf(r))
	respond(w, keys, err)
}

func (a *api) addSigningKey(w http.ResponseWriter, r *http.Request) {
	var in core.SigningKeyInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	k, err := a.core.AddSigningKey(r.Context(), actorOf(r), in)
	respondStatus(w, http.StatusCreated, k, err)
}

func (a *api) deleteSigningKey(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteSigningKey(r.Context(), actorOf(r), chi.URLParam(r, "id")))
}

func (a *api) entityAgentActions(w http.ResponseWriter, r *http.Request) {
	out, err := a.core.EntityAgentActions(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"))
	respond(w, out, err)
}

func (a *api) createAgentJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action string `json:"action"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	j, err := a.core.CreateAgentJob(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"), in.Action)
	respondStatus(w, http.StatusCreated, j, err)
}

func (a *api) signAgentJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		KeyID     string `json:"keyId"`
		Signature string `json:"signature"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	j, err := a.core.SignAgentJob(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.KeyID, in.Signature)
	respond(w, j, err)
}

// agentRelease serves an agent binary for self-updates (agent token).
func (a *api) agentRelease(w http.ResponseWriter, r *http.Request) {
	file, err := a.core.AgentRelease(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), chi.URLParam(r, "tag"), chi.URLParam(r, "asset"))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, file)
}

func (a *api) agentReport(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	// Lenient: newer agents may send more fields.
	var in core.Report
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&in); err != nil {
		writeErr(w, &core.InvalidError{Key: "validation.json", Msg: "invalid JSON: " + err.Error()})
		return
	}
	respond(w, map[string]bool{"ok": true}, a.core.AgentReport(r.Context(), token, in))
}

func (a *api) listAgents(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.Agents(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, list, err)
}

func (a *api) createEnrollment(w http.ResponseWriter, r *http.Request) {
	e, err := a.core.CreateEnrollment(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respondStatus(w, http.StatusCreated, e, err)
}

func (a *api) revokeAgent(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.RevokeAgent(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}

func (a *api) listPublications(w http.ResponseWriter, r *http.Request) {
	ps, err := a.core.Publications(r.Context(), actorOf(r))
	respond(w, ps, err)
}

func (a *api) createPublication(w http.ResponseWriter, r *http.Request) {
	var in core.PublicationInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, err := a.core.CreatePublication(r.Context(), actorOf(r), in)
	respondStatus(w, http.StatusCreated, p, err)
}

func (a *api) updatePublication(w http.ResponseWriter, r *http.Request) {
	var in core.PublicationInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, err := a.core.UpdatePublication(r.Context(), actorOf(r), chi.URLParam(r, "id"), in)
	respond(w, p, err)
}

func (a *api) deletePublication(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeletePublication(r.Context(), actorOf(r), chi.URLParam(r, "id")))
}

func (a *api) listReleases(w http.ResponseWriter, r *http.Request) {
	out, err := a.core.Releases(r.Context(), actorOf(r), chi.URLParam(r, "id"))
	respond(w, out, err)
}

func (a *api) createRelease(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note string `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	rl, err := a.core.CreateRelease(r.Context(), actorOf(r), chi.URLParam(r, "id"), in.Note)
	respondStatus(w, http.StatusCreated, rl, err)
}

func (a *api) listTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ts, err := a.core.Tasks(r.Context(), actorOf(r), chi.URLParam(r, "p"), q.Get("board"), q.Get("column"), q.Get("assignee"), q.Get("open") == "true")
	respond(w, ts, err)
}

func (a *api) createTask(w http.ResponseWriter, r *http.Request) {
	var in core.TaskInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	t, err := a.core.CreateTask(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
	respond(w, t, err)
}

func (a *api) updateTask(w http.ResponseWriter, r *http.Request) {
	var in core.TaskPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	t, err := a.core.UpdateTask(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, t, err)
}

func (a *api) deleteTask(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteTask(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
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
	d, err := a.core.LocalizedDocument(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), r.URL.Query().Get("lang"))
	respond(w, d, err)
}

func (a *api) getTranslation(w http.ResponseWriter, r *http.Request) {
	t, err := a.core.Translation(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), chi.URLParam(r, "lang"))
	respond(w, t, err)
}

func (a *api) saveTranslation(w http.ResponseWriter, r *http.Request) {
	var in core.TranslationInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	t, err := a.core.SaveTranslation(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), chi.URLParam(r, "lang"), in)
	respond(w, t, err)
}

func (a *api) deleteTranslation(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteTranslation(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), chi.URLParam(r, "lang")))
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

func (a *api) importMarkdown(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 50<<20))
	if err != nil {
		writeErr(w, core.Invalid("validation.import_size", nil, "the upload is larger than 50 MB"))
		return
	}
	res, err := a.core.ImportMarkdown(r.Context(), actorOf(r), chi.URLParam(r, "p"), data)
	respond(w, res, err)
}

func (a *api) importOpenAPI(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 5<<20))
	if err != nil {
		writeErr(w, core.Invalid("validation.import_size", nil, "the upload is too large"))
		return
	}
	res, err := a.core.ImportOpenAPI(r.Context(), actorOf(r), chi.URLParam(r, "p"), data)
	respond(w, res, err)
}

func (a *api) duplicates(w http.ResponseWriter, r *http.Request) {
	out, err := a.core.Duplicates(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, out, err)
}

func (a *api) mergeEntities(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Keep   string `json:"keep"`
		Remove string `json:"remove"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	e, err := a.core.MergeEntities(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Keep, in.Remove)
	respond(w, e, err)
}

func (a *api) topology(w http.ResponseWriter, r *http.Request) {
	out, err := a.core.Topology(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, out, err)
}

func (a *api) adminEvents(w http.ResponseWriter, r *http.Request) {
	out, err := a.core.AuditLog(r.Context(), actorOf(r), "", core.ParseAuditQuery(r.URL.Query().Get))
	respond(w, out, err)
}

func (a *api) projectEvents(w http.ResponseWriter, r *http.Request) {
	out, err := a.core.AuditLog(r.Context(), actorOf(r), chi.URLParam(r, "p"), core.ParseAuditQuery(r.URL.Query().Get))
	respond(w, out, err)
}

func (a *api) softwareTree(w http.ResponseWriter, r *http.Request) {
	t, err := a.core.SoftwareTree(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"))
	respond(w, t, err)
}

func (a *api) diagnose(w http.ResponseWriter, r *http.Request) {
	d, err := a.core.Diagnose(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"))
	respond(w, d, err)
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
