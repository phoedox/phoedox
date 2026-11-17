package server

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/phoedox/phoedox/internal/aichat"
	"github.com/phoedox/phoedox/internal/backup"
	"github.com/phoedox/phoedox/internal/botcheck"
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
	"github.com/phoedox/phoedox/internal/publish"
	"github.com/phoedox/phoedox/internal/store"
)

type api struct {
	core    *core.Service
	chat    *aichat.Service
	auth    *auth.Auth
	cfg     config.Config
	limiter *limiter
	// regLimiter limits open OAuth client registrations per IP.
	regLimiter *limiter
	// signupIP and signupMail limit sign-ups per client address and per
	// e-mail address (so nobody can flood someone's mailbox).
	signupIP, signupMail *limiter
	// runLimiter limits request block runs per user.
	runLimiter *limiter
	// loginFails counts failed sign-ins for the bot check.
	loginFails failures
	// feedbackIP limits feedback reports per client address.
	feedbackIP *limiter
	// feedbackMail limits inbound mails per portal address.
	feedbackMail *limiter
	// sharedAppIP limits reads of public apps per client address.
	sharedAppIP *limiter
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
	a.botRoutes(r)
	// Server-to-server: a service redeems a sign-in code (ADR-0058).
	r.Post("/identity/token", a.identityToken)
	r.Post("/app/token", a.appToken)
	r.Post("/auth/logout", a.logout)
	r.Get("/status/{token}", a.publicStatus)
	// Feedback portals: public page data, reports, status links.
	r.Get("/feedback/{handle}", a.publicPortal)
	r.Post("/feedback/{handle}/reports", a.submitFeedback)
	r.Get("/feedback/login/callback", a.reporterCallback)
	r.Post("/feedback/inbound/{key}", a.inboundMail)
	r.Get("/feedback/{handle}/login", a.reporterLogin)
	r.With(requireHeader).Delete("/feedback/{handle}/login", a.reporterLogout)
	// Public view: released topics, votes, following, moderated comments.
	r.Get("/feedback/{handle}/topics", a.publicTopics)
	r.Get("/feedback/{handle}/topics/{id}", a.publicTopic)
	r.Post("/feedback/{handle}/topics/{id}/vote", a.voteTopic)
	r.Post("/feedback/{handle}/topics/{id}/follow", a.followTopic)
	r.Delete("/feedback/{handle}/topics/{id}/follow", a.unfollowTopic)
	r.Post("/feedback/{handle}/topics/{id}/comments", a.commentTopic)
	r.With(requireHeader).Delete("/feedback/unfollow/{token}", a.unfollowByLink)
	// Public apps: the frame, reads from the snapshot, AI within the limit.
	r.Get("/shared-apps/{token}", a.openSharedApp)
	r.Post("/shared-apps/{token}/read", a.readSharedApp)
	r.Post("/shared-apps/{token}/ai", a.askSharedApp)
	r.Get("/shared-apps/{token}/preview.png", a.sharedAppPreview)
	// Pull request events of a project's code connection (signed).
	r.Post("/hooks/code/{id}", a.codeHook)
	// Payment provider events (signed; ADR-0079).
	r.Post("/payments/webhook/{provider}", a.paymentWebhook)
	r.Get("/payments/pay/{ref}", a.payInfo)
	// Short-lived download links of project exports (sealed token).
	r.Get("/exports/{token}", a.openExportLink)
	// Fonts and logos of themes (public: sign-in pages, publications, e-mails).
	r.Get("/theme-assets/{file}", a.themeAsset)
	r.Get("/feedback/status/{token}", a.feedbackStatus)
	r.With(requireHeader).Post("/feedback/status/{token}/reply", a.replyFeedbackStatus)
	r.With(requireHeader).Delete("/feedback/status/{token}", a.deleteFeedbackStatus)
	r.Get("/feedback/status/{token}/export", a.exportFeedbackStatus)
	// One-time secret links: POST so link previews cannot burn them.
	r.Post("/secret-links/{id}/open", a.openSecretLink)
	r.Get("/instance", func(w http.ResponseWriter, req *http.Request) {
		info := map[string]any{"version": Version, "domain": a.cfg.Domain, "passwordReset": a.resetAvailable(), "signup": a.signupAvailable(req.Context())}
		if a.cfg.TermsURL != "" {
			info["termsUrl"] = a.cfg.TermsURL
		}
		if a.cfg.PrivacyURL != "" {
			info["privacyUrl"] = a.cfg.PrivacyURL
		}
		// On an organization's own app domain the app shows its name.
		if c := a.core.AppContextFor(req.Context(), req.Host); c.Org != nil {
			info["org"] = c.Org
		}
		if a.auth.OIDC != nil {
			info["oidc"] = map[string]string{"name": a.auth.OIDC.Config.Name}
		}
		if a.auth.HA.Enabled() {
			info["homeassistant"] = true
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
	r.Get("/agent/ws", a.agentWS)
	r.Post("/agent/jobs/{id}/result", a.agentJobResult)
	r.Get("/agent/releases/{tag}/{asset}", a.agentRelease)
	r.Get("/agent/terminal/{id}", a.terminalAgent)
	r.Get("/agent/speedtest/ping", a.agentOnly(a.speedPing))
	r.Get("/agent/speedtest/down", a.agentOnly(a.speedDown))
	r.Post("/agent/speedtest/up", a.agentOnly(a.speedUp))
	r.Get("/shared/{token}/files/{id}", a.sharedFile)
	r.Get("/shared/{token}/files/{id}/{name}", a.sharedFile)
	r.Get("/auth/oidc/start", a.oidcStart)
	r.Get("/auth/oidc/callback", a.oidcCallback)
	r.Get("/phoedox-skill.zip", a.phoedoxSkill)
	r.Get("/auth/ha/start", a.haStart)
	r.Get("/auth/ha/callback", a.haCallback)
	r.Group(func(r chi.Router) {
		r.Use(requireHeader)
		r.Post("/auth/reset-request", a.resetRequest)
		r.Post("/auth/signup", a.signup)
		r.Get("/identity/request", a.identityRequest)
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
		r.With(requireFull).Post("/identity/approve", a.identityApprove)
		r.Get("/me/notifications", a.listNotifications)
		r.Get("/me/tasks", a.myTasks)
		r.Get("/me/usage", a.myUsage)
		r.Get("/me/feedback", a.myFeedback)
		r.Get("/me/purchases", a.myPurchases)
		r.Post("/me/payments/portal", a.paymentPortal)
		r.Get("/me/ai-credits", a.myCredits)
		r.Post("/me/ai-credits/buy", a.buyCredits)
		r.Get("/admin/search/embeddings", a.adminEmbeddings)
		r.Put("/admin/search/embeddings", a.setAdminEmbeddings)
		r.Get("/admin/ai-credits", a.adminCredits)
		r.Put("/admin/ai-credits/offers", a.setCreditOffers)
		r.Get("/admin/feedback-storage/offers", a.feedbackStorageOffers)
		r.Put("/admin/feedback-storage/offers", a.setFeedbackStorageOffers)
		r.Post("/admin/ai-credits/grants", a.grantCredits)
		r.Get("/admin/dpa", a.adminDPA)
		r.Post("/admin/dpa", a.publishDPA)
		r.Get("/admin/payments", a.adminPayments)
		r.Put("/admin/payments", a.setAdminPayments)
		r.Post("/admin/payments/check", a.checkPayments)
		r.Post("/admin/payments/test", a.testPurchase)
		r.Get("/admin/payments/purchases", a.adminPurchases)
		r.Post("/admin/payments/purchases/{id}/refund", a.refundPurchase)
		r.Get("/themes", a.themes)
		r.Get("/themes/{id}", a.getTheme)
		r.Post("/themes/check", a.checkTheme)
		r.Get("/store/items", a.storeItems)
		r.Get("/store/items/{id}/versions", a.storeItemVersions)
		r.Get("/me/features", a.myFeatures)
		r.Get("/me/skills", a.mySkills)
		r.Get("/me/home", a.home)
		r.Put("/me/pinned", a.setPinned)
		r.Get("/me/devices", a.sharedDevices)
		r.Post("/app-shares/{token}/copy", a.copySharedApp)
		r.Post("/me/devices/{grant}/control", a.controlShared)
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
			r.Get("/me/export", a.myExport)
			r.Post("/me/export", a.requestExport)
			r.Get("/me/exports/{id}/download", a.downloadExport)
			r.Get("/me/deletion", a.accountDeletion)
			r.Post("/me/delete", a.deleteAccount)
			r.Get("/me/security", a.security)
			r.Get("/me/notification-settings", a.notificationSettings)
			r.Put("/me/notification-settings", a.saveNotificationSettings)
			r.Post("/secret-links", a.createSecretLink)
			r.Get("/me/recent", a.recent)
			r.Post("/qr", a.qr)
			r.Get("/speedtest/ping", a.speedPing)
			r.Get("/speedtest/down", a.speedDown)
			r.Post("/speedtest/up", a.speedUp)
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
			r.Put("/me/theme", a.setMyTheme)
			r.Post("/themes", a.createTheme)
			r.Put("/themes/{id}", a.updateTheme)
			r.Delete("/themes/{id}", a.deleteTheme)
			r.Post("/themes/{id}/release", a.releaseTheme)
			r.Post("/themes/generate", a.generateTheme)
			r.Post("/store/themes", a.installStoreTheme)
			r.Post("/themes/assets", a.uploadThemeAsset)
			r.Post("/oauth/authorize", a.oauthAuthorize)
			r.Delete("/oauth/grants/{id}", a.revokeGrant)
		})
		r.Get("/oauth/grants", a.listGrants)
		r.Route("/admin", func(r chi.Router) {
			r.Use(requireFull)
			r.Get("/usage", a.instanceUsage)
			r.Get("/bot-protection", a.botSettings)
			r.Put("/bot-protection", a.saveBotSettings)
			r.Put("/theme", a.setInstanceTheme)
			r.Get("/apps", a.instanceAppPolicy)
			r.Put("/apps", a.setInstanceAppPolicy)
			r.Get("/user-content", a.userContent)
			r.Put("/user-content", a.saveUserContent)
			r.Get("/orgs/{o}/prices", a.orgPrices)
			r.Post("/orgs/{o}/prices", a.setOrgPrice)
			r.Post("/orgs/{o}/prices/preview", a.previewOrgPrice)
			r.Delete("/orgs/{o}/prices/{id}", a.deleteOrgPrice)
			r.Get("/backups", a.listBackups)
			r.Post("/backups", a.createBackup)
			r.Post("/backups/upload", a.uploadBackup)
			r.Get("/backups/{name}", a.downloadBackup)
			r.Delete("/backups/{name}", a.deleteBackup)
			r.Post("/backups/{name}/restore", a.restoreBackup)
			r.Get("/settings", a.getSettings)
			r.Put("/settings", a.putSettings)
			r.Get("/ai/providers", a.listAIProviders)
			r.Post("/ai/providers", a.saveAIProvider)
			r.Put("/ai/providers/{id}", a.saveAIProvider)
			r.Delete("/ai/providers/{id}", a.deleteAIProvider)
			r.Post("/ai/providers/{id}/models", a.discoverAIModels)
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
			r.Get("/{o}/features", a.orgFeatures)
			r.Get("/{o}/ai-chat", a.orgAIChat)
			r.Get("/{o}/usage", a.orgUsage)
			r.Get("/{o}/ai/members", a.memberBudgets)
			r.Put("/{o}/ai/members/{user}", a.setMemberLimit)
			r.Get("/{o}/theme", a.orgTheme)
			r.Get("/{o}/background-ai", a.orgBackgroundAI)
			r.Put("/{o}/background-ai", a.setOrgBackgroundAI)
			r.Get("/{o}/ai-credits", a.orgCredits)
			r.Post("/{o}/ai-credits/buy", a.buyCredits)
			r.Get("/{o}/dpa", a.orgDPA)
			r.Post("/{o}/dpa/accept", a.acceptDPA)
			r.Get("/{o}/dpa/acceptances/{id}/pdf", a.dpaPDF)
			r.Get("/{o}/providers", a.orgProviders)
			r.Post("/{o}/providers", a.grantProvider)
			r.Patch("/{o}/providers/{id}", a.patchProvider)
			r.Delete("/{o}/providers/{id}", a.revokeProvider)
			r.Get("/{o}/customers", a.customerOverview)
			r.Get("/{o}/apps", a.orgAppPolicy)
			r.Put("/{o}/apps", a.setOrgAppPolicy)
			r.Get("/{o}/templates", a.orgTemplates)
			r.Put("/{o}/templates", a.setOrgTemplates)
			r.Get("/{o}/customers/time.csv", a.customerTimeCSV)
			r.Get("/{o}/store-access", a.orgStoreAccess)
			r.Put("/{o}/store-access", a.setOrgStoreAccess)
			r.Get("/{o}/store/requests", a.orgStoreRequests)
			r.Post("/{o}/store/requests/{id}", a.decideStoreRequest)
			r.Put("/{o}/theme", a.setOrgTheme)
			r.Post("/{o}/ai/requests", a.requestBudget)
			r.Post("/{o}/ai/requests/{id}", a.decideBudget)
			r.Get("/{o}/ai/providers", a.listAIProviders)
			r.Post("/{o}/ai/providers", a.saveAIProvider)
			r.Put("/{o}/ai/providers/{id}", a.saveAIProvider)
			r.Delete("/{o}/ai/providers/{id}", a.deleteAIProvider)
			r.Post("/{o}/ai/providers/{id}/models", a.discoverAIModels)
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
			writeJSON(w, http.StatusOK, a.core.ProjectTemplates(r.Context(), actorOf(r), r.URL.Query().Get("lang")))
		})
		r.Post("/projects", a.createProject)
		r.Post("/projects/import", a.importProject)
		r.Post("/projects/import-remote", a.importRemote)
		r.Post("/projects/import-remote/list", a.remoteProjects)
		r.Get("/trashed-projects", a.trashedProjects)
		r.With(requireFull).Post("/trashed-projects/{p}/restore", a.restoreProject)
		r.Route("/projects/{p}", func(r chi.Router) {
			r.Get("/", a.getProject)
			r.Patch("/", a.updateProject)
			r.With(requireFull).Delete("/", a.deleteProject)
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
			r.Post("/documents/{d}/decide", a.decideDocument)
			r.Get("/documents/{d}/tasks", a.documentTasks)
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
			r.Put("/documents/{d}/draft/code", a.linkDraftCode)
			r.Delete("/documents/{d}/draft/code", a.unlinkDraftCode)
			r.Put("/documents/{d}/whiteboard", a.saveWhiteboard)
			r.Post("/documents/{d}/whiteboard/ops", a.whiteboardOps)
			r.Post("/documents/{d}/whiteboard/comments", a.whiteboardComment)
			r.Get("/documents/{d}/prototypes/{id}/data", a.prototypeData)
			r.Get("/documents/{d}/prototypes/{id}/export", a.prototypeExport)
			r.Post("/documents/{d}/prototypes/{id}/app", a.prototypeApp)
			r.Get("/embed-preview", a.embedPreview)
			r.Get("/embed-policy", a.embedPolicy)
			r.Post("/whiteboards/code-status", a.whiteboardCodeStatus)
			r.Post("/whiteboards/mermaid", a.whiteboardMermaid)
			r.Post("/documents/{d}/whiteboard/neighbourhood", a.whiteboardNeighbourhood)
			r.Post("/documents/{d}/draft/review", a.requestReview)
			r.Post("/documents/{d}/draft/reject", a.rejectReview)
			r.Get("/documents/{d}/skill", a.pageSkill)
			r.Put("/documents/{d}/skill", a.setPageSkill)
			r.Delete("/documents/{d}/skill", a.removePageSkill)
			r.Get("/skills", a.listSkills)
			r.Get("/skills.zip", a.downloadSkills)
			r.Get("/skills/{name}", a.getSkill)
			r.Get("/skills/{name}/download", a.downloadSkills)
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
			r.Get("/export/link", a.exportLink)
			r.Get("/offline", a.offlineCopy)
			r.Post("/answer", a.answer)
			r.Get("/rules", a.listRules)
			r.Post("/rules", a.saveRule)
			r.Put("/rules/{id}", a.saveRule)
			r.Delete("/rules/{id}", a.deleteRule)
			r.Post("/rules/{id}/test", a.testRule)
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
			r.Put("/category-names", a.setCategoryName)
			r.Post("/category-names/rename", a.renameCategoryNames)
			r.Get("/history/points", a.projectPoints)
			r.Get("/dbschema", a.dbSchema)
			r.Post("/find", a.findInDocuments)
			r.Post("/validate-blocks", a.validateBlocks)
			r.Get("/history/at", a.projectAt)
			r.Post("/history/rollback", a.rollbackProject)
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
			r.Get("/live", a.projectLive)
			r.Get("/ai/status", a.chatStatus)
			r.Get("/ai/models", a.chatModels)
			r.Get("/ai/chats", a.listChats)
			r.Post("/ai/chats", a.createChat)
			r.Get("/ai/chats/{c}", a.getChat)
			r.Delete("/ai/chats/{c}", a.deleteChat)
			r.Post("/ai/chats/{c}/messages", a.sendChat)
			r.Post("/ai/chats/{c}/proposals/{tool}", a.decideChatProposal)
			r.Post("/boards/{d}/tasks", a.createTask)
			r.Patch("/tasks/{id}", a.updateTask)
			r.Delete("/tasks/{id}", a.deleteTask)
			r.Get("/tasks/{id}/time", a.taskTime)
			r.Post("/tasks/{id}/time", a.logTime)
			r.Delete("/time/{id}", a.deleteTime)
			r.Get("/time", a.projectTime)
			r.Get("/time.csv", a.timeCSV)
			r.Get("/tasks/{id}/code", a.taskCode)
			r.Post("/tasks/{id}/code", a.linkTaskCode)
			r.Delete("/tasks/{id}/code/{link}", a.unlinkTaskCode)
			r.Get("/code/repos", a.codeRepos)
			r.Post("/code/repos", a.addCodeRepo)
			r.Delete("/code/repos/{id}", a.deleteCodeRepo)
			r.Get("/code/repos/{id}/commits", a.codeCommits)
			r.Get("/code/repos/{id}/releases", a.codeReleases)
			r.Get("/code/repos/{id}/webhook", a.codeRepoWebhook)
			r.Post("/code/repos/{id}/webhook", a.codeRepoWebhook)
			r.Get("/code/docs-changes", a.pullRequestDocs)
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
			r.Get("/usage", a.projectUsage)
			r.Get("/feedback/portals", a.feedbackPortals)
			r.Post("/feedback/portals", a.saveFeedbackPortal)
			r.Put("/feedback/portals/{id}", a.saveFeedbackPortal)
			r.Delete("/feedback/portals/{id}", a.deleteFeedbackPortal)
			r.Post("/feedback/portals/{id}/keys", a.rotateFeedbackKey)
			r.Put("/feedback/portals/{id}/domain", a.setPortalDomain)
			r.Post("/feedback/portals/{id}/domain/verify", a.verifyPortalDomain)
			r.Put("/feedback/portals/{id}/moderators", a.setFeedbackModerators)
			r.Get("/feedback/topics", a.feedbackTopics)
			r.Get("/feedback/topics/{id}", a.feedbackTopic)
			r.Patch("/feedback/topics/{id}", a.updateFeedbackTopic)
			r.Post("/feedback/topics/{id}/merge", a.mergeFeedbackTopic)
			r.Post("/feedback/topics/{id}/comments", a.teamCommentTopic)
			r.Post("/feedback/topics/{id}/comments/{cid}/{action}", a.decideTopicComment)
			r.Post("/feedback/topics/{id}/tickets", a.feedbackToBoard)
			r.Post("/feedback/topics/{id}/forward", a.forwardFeedbackTopic)
			r.Post("/feedback/topics/{id}/forwards/refresh", a.refreshFeedbackForwards)
			r.Get("/feedback/trackers", a.feedbackTrackers)
			r.Get("/feedback/storage", a.feedbackStorage)
			r.Post("/feedback/storage/buy", a.buyFeedbackStorage)
			r.Post("/feedback/trackers", a.saveFeedbackTracker)
			r.Put("/feedback/trackers/{id}", a.saveFeedbackTracker)
			r.Delete("/feedback/trackers/{id}", a.deleteFeedbackTracker)
			r.Post("/feedback/topics/{id}/answer-draft", a.feedbackAnswerDraft)
			r.Get("/feedback/entities/{e}", a.entityFeedback)
			r.Post("/tasks/{id}/feedback-seen", a.taskFeedbackSeen)
			r.Post("/feedback/reports/{id}/move", a.moveFeedbackReport)
			r.Get("/feedback/attachments/{id}", a.feedbackAttachment)
			r.Put("/feedback/quota", a.setFeedbackQuota)
			r.Put("/theme", a.setProjectTheme)
			r.Get("/autolinks", a.autoLinks)
			r.Get("/background-ai", a.backgroundAI)
			r.Put("/background-ai", a.setBackgroundAI)
			r.Get("/snapshots", a.snapshots)
			r.Get("/glossary", a.glossary)
			r.Get("/code/dbmodel", a.repoDBModel)
			r.Put("/glossary", a.setGlossary)
			r.Get("/documents/{d}/glossary-check", a.glossaryCheck)
			r.Post("/snapshots", a.createSnapshot)
			r.Get("/snapshots/{name}", a.snapshot)
			r.Delete("/snapshots/{name}", a.deleteSnapshot)
			r.Get("/snapshots/{name}/compare", a.compareSnapshots)
			r.Put("/autolinks", a.setAutoLinks)
			r.Post("/apps/preview", a.previewApp)
			r.Post("/apps/{doc}/open", a.openApp)
			r.Put("/apps/{doc}/grant", a.grantApp)
			r.Post("/apps/{doc}/read", a.readApp)
			r.Get("/apps/{doc}/storage", a.appStorageList)
			r.Get("/apps/{doc}/storage/{key}", a.appStorageGet)
			r.Put("/apps/{doc}/storage/{key}", a.appStorageSet)
			r.Delete("/apps/{doc}/storage/{key}", a.appStorageDelete)
			r.Post("/apps/{doc}/console", a.appConsole)
			r.Post("/apps/{doc}/ai", a.appAI)
			r.Get("/apps/{doc}/share", a.getAppShare)
			r.Put("/apps/{doc}/share", a.publishApp)
			r.Post("/apps/{doc}/share/refresh", a.refreshAppShare)
			r.Delete("/apps/{doc}/share", a.unpublishApp)
			r.Post("/store/apps", a.installStoreApp)
			r.Get("/app-templates", a.appTemplates)
			r.Put("/apps/{doc}/template", a.setAppTemplate)
			r.Post("/apps/{doc}/copy", a.copyApp)
			r.Get("/app-gallery", a.appGallery)
			r.Get("/apps/{doc}/me", a.appMe)
			r.Get("/apps/{doc}/events", a.appEvents)
			r.Post("/apps/{doc}/db/{coll}/query", a.appQuery)
			r.Post("/apps/{doc}/db/{coll}", a.appAddRecord)
			r.Put("/apps/{doc}/db/{coll}/{id}", a.appPutRecord)
			r.Delete("/apps/{doc}/db/{coll}/{id}", a.appDeleteRecord)
			r.Get("/apps/{doc}/uploads", a.appUploads)
			r.Post("/apps/{doc}/uploads", a.appUpload)
			r.Get("/apps/{doc}/uploads/{id}", a.appUploadContent)
			r.Delete("/apps/{doc}/uploads/{id}", a.appDeleteUpload)
			r.Put("/apps/{doc}/devices/{device}", a.appDevice)
			r.Get("/apps/{doc}/self", a.appOwnFiles)
			r.Put("/apps/{doc}/self", a.appSelfSave)
			r.Get("/apps/{doc}/assets", a.appAssets)
			r.Post("/apps/{doc}/assets", a.addAppAsset)
			r.Post("/apps/{doc}/assets/copy", a.copyAppAssets)
			r.Delete("/apps/{doc}/assets/{name}", a.deleteAppAsset)
			r.Put("/apps/{doc}/preview", a.setAppPreview)
			r.Get("/apps/{doc}/preview.png", a.appPreview)
			r.Get("/store/requests", a.myStoreRequests)
			r.Post("/store/requests", a.requestStoreItem)
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
			r.Get("/entities/{e}/readings", a.entityReadings)
			r.Get("/entities/{e}/timeline", a.entityTimeline)
			r.Get("/documents/{d}/whiteboard.svg", a.whiteboardSVG)
			r.Get("/discovered", a.discovered)
			r.Post("/discovered/{id}/adopt", a.adoptDiscovered)
			r.Post("/discovered/{id}/dismiss", a.dismissDiscovered)
			r.Post("/readings", a.recordReadings)
			r.Get("/entities/{e}/device", a.device)
			r.Put("/entities/{e}/device", a.setDevice)
			r.Get("/entities/{e}/device/log", a.deviceLog)
			r.Post("/entities/{e}/device/control", a.controlDevice)
			r.Post("/entities/{e}/device/grants", a.grantControl)
			r.Delete("/entities/{e}/device/grants/{id}", a.revokeControl)
			r.Get("/entities/{e}/meta", a.entityMeta)
			r.Put("/entities/{e}/meta", a.setEntityMeta)
			r.Post("/entities/{e}/reviewed", a.entityReviewed)
			r.Put("/kinds/{k}/rules", a.setKindRules)
			r.Get("/locations", a.locations)
			r.Post("/query", a.runQuery)
			r.Get("/queries", a.listQueries)
			r.Post("/queries", a.saveQuery)
			r.Put("/queries/{q}", a.saveQuery)
			r.Delete("/queries/{q}", a.deleteQuery)
			r.Get("/queries/{q}/run", a.runSavedQuery)
			r.Post("/entities/bulk", a.bulkEntities)
			r.Get("/household", a.household)
			r.Get("/ipam", a.ipam)
			r.Get("/terminal/{id}", a.terminalBrowser)
			r.Put("/household/price", a.setPowerPrice)
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
	var in struct {
		Email, Password string
		BotToken        string `json:"botToken"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	// After failures, the bot check comes before the next try.
	if a.signinNeedsBot(r, in.Email) && !a.botCheck(w, r, botcheck.PlaceSignin, in.BotToken) {
		return
	}
	step, err := a.auth.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		if errors.Is(err, auth.ErrBadLogin) {
			a.loginFails.add(loginKeys(r, in.Email)...)
		}
		writeErr(w, err)
		return
	}
	a.loginFails.clear(loginKeys(r, in.Email)...)
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
	list := a.core.Projects
	if r.URL.Query().Get("all") == "1" {
		list = a.core.AllProjects
	}
	ps, err := list(r.Context(), actorOf(r))
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

// deleteProject moves a project to the trash after the owner typed its
// address and confirmed with the password (or an authenticator code).
func (a *api) deleteProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm  string `json:"confirm"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	actor := actorOf(r)
	if err := a.auth.ConfirmIdentity(r.Context(), actor, in.Password, in.Code); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.core.DeleteProject(r.Context(), actor, chi.URLParam(r, "p"), in.Confirm); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) trashedProjects(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.TrashedProjects(r.Context(), actorOf(r))
	respond(w, list, err)
}

func (a *api) restoreProject(w http.ResponseWriter, r *http.Request) {
	p, err := a.core.RestoreProject(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, p, err)
}

func (a *api) discovered(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.Discovered(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("all") == "1")
	respond(w, list, err)
}

func (a *api) adoptDiscovered(w http.ResponseWriter, r *http.Request) {
	var in core.AdoptInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.AdoptDiscovered(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, res, err)
}

func (a *api) dismissDiscovered(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Dismissed bool `json:"dismissed"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := a.core.DismissDiscovered(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.Dismissed); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// whiteboardSVG draws a whiteboard read-only (```whiteboard blocks in pages).
func (a *api) whiteboardSVG(w http.ResponseWriter, r *http.Request) {
	d, err := a.core.Document(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	if err != nil {
		writeErr(w, err)
		return
	}
	wb, err := core.WhiteboardOf(d)
	if err != nil || d.Type != store.DocWhiteboard {
		writeErr(w, core.ErrNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self' data:")
	w.Header().Set("Cache-Control", "private, no-cache")
	_, _ = io.WriteString(w, publish.WhiteboardSVG(wb, d.Title, "/api/v1/files/"))
}

func timeFilter(r *http.Request) store.TimeFilter {
	q := r.URL.Query()
	return store.TimeFilter{From: q.Get("from"), To: q.Get("to"), UserID: q.Get("user"), BoardID: q.Get("board")}
}

func (a *api) taskTime(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.TimeEntries(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), store.TimeFilter{})
	respond(w, list, err)
}

func (a *api) logTime(w http.ResponseWriter, r *http.Request) {
	var in core.TimeInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	e, err := a.core.LogTime(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respondStatus(w, http.StatusCreated, e, err)
}

func (a *api) deleteTime(w http.ResponseWriter, r *http.Request) {
	if err := a.core.DeleteTime(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) projectTime(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.TimeEntries(r.Context(), actorOf(r), chi.URLParam(r, "p"), "", timeFilter(r))
	respond(w, list, err)
}

func (a *api) timeCSV(w http.ResponseWriter, r *http.Request) {
	b, err := a.core.TimeReportCSV(r.Context(), actorOf(r), chi.URLParam(r, "p"), timeFilter(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="time-`+chi.URLParam(r, "p")+`.csv"`)
	_, _ = w.Write(b)
}

func (a *api) entityTimeline(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := a.core.EntityTimeline(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"), limit)
	respond(w, items, err)
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

func (a *api) taskCode(w http.ResponseWriter, r *http.Request) {
	l, err := a.core.TaskCode(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, l, err)
}

func (a *api) linkTaskCode(w http.ResponseWriter, r *http.Request) {
	var in core.CodeLinkInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	l, err := a.core.LinkTaskCode(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, l, err)
}

func (a *api) unlinkTaskCode(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.UnlinkTaskCode(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), chi.URLParam(r, "link")))
}

func (a *api) codeRepos(w http.ResponseWriter, r *http.Request) {
	l, err := a.core.CodeRepos(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, l, err)
}

func (a *api) addCodeRepo(w http.ResponseWriter, r *http.Request) {
	var in core.CodeRepoInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	x, err := a.core.AddCodeRepo(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, x, err)
}

func (a *api) deleteCodeRepo(w http.ResponseWriter, r *http.Request) {
	respond(w, map[string]bool{"ok": true}, a.core.DeleteCodeRepo(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}

func (a *api) codeCommits(w http.ResponseWriter, r *http.Request) {
	l, err := a.core.CodeCommits(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), r.URL.Query().Get("q"))
	respond(w, l, err)
}

func (a *api) codeReleases(w http.ResponseWriter, r *http.Request) {
	l, err := a.core.CodeReleases(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, l, err)
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

func (a *api) documentTasks(w http.ResponseWriter, r *http.Request) {
	ts, err := a.core.DocumentTasks(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"))
	respond(w, ts, err)
}

func (a *api) decideDocument(w http.ResponseWriter, r *http.Request) {
	var in core.DecideInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.Decide(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "d"), in)
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
	// Archives with images (Notion, Confluence, BookStack) can be large.
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 300<<20))
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

// historyTime reads a point in time (RFC 3339).
func historyTime(v string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}, core.Invalid("validation.history_time", nil, "give the point in time as RFC 3339")
	}
	return t, nil
}

func (a *api) projectPoints(w http.ResponseWriter, r *http.Request) {
	var before time.Time
	if v := r.URL.Query().Get("before"); v != "" {
		t, err := historyTime(v)
		if err != nil {
			writeErr(w, err)
			return
		}
		before = t
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := a.core.ProjectPoints(r.Context(), actorOf(r), chi.URLParam(r, "p"), r.URL.Query().Get("doc"), before, limit)
	respond(w, list, err)
}

func (a *api) findInDocuments(w http.ResponseWriter, r *http.Request) {
	var in core.FindInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.FindInDocuments(r.Context(), actorOf(r), chi.URLParam(r, "p"), in)
	respond(w, res, err)
}

func (a *api) dbSchema(w http.ResponseWriter, r *http.Request) {
	list, err := a.core.DBSchema(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, list, err)
}

func (a *api) validateBlocks(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Document string  `json:"document"`
		Markdown *string `json:"markdown"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	list, err := a.core.ValidateBlocks(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Document, in.Markdown)
	respond(w, list, err)
}

func (a *api) projectAt(w http.ResponseWriter, r *http.Request) {
	at, err := historyTime(r.URL.Query().Get("at"))
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.ProjectStateAt(r.Context(), actorOf(r), chi.URLParam(r, "p"), at)
	respond(w, res, err)
}

func (a *api) rollbackProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		At string `json:"at"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	at, err := historyTime(in.At)
	if err != nil {
		writeErr(w, err)
		return
	}
	res, err := a.core.RollbackProject(r.Context(), actorOf(r), chi.URLParam(r, "p"), at)
	respond(w, res, err)
}

func (a *api) setCategoryName(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Category string `json:"category"`
		Lang     string `json:"lang"`
		Name     string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	p, err := a.core.SetCategoryName(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Category, in.Lang, in.Name)
	respond(w, p, err)
}

func (a *api) renameCategoryNames(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, map[string]bool{"ok": true}, a.core.RenameCategoryNames(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.From, in.To))
}
