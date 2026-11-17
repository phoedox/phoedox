package server

import (
	"encoding/json"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/phoedox/phoedox/internal/botcheck"
	"github.com/phoedox/phoedox/internal/core"
)

// Feedback portals: the public side (portal, reports, status link) and the
// moderators' side.

func (a *api) publicPortal(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")
	v, err := a.core.PublicFeedbackPortal(r.Context(), handle, r.URL.Query().Get("lang"))
	if err == nil && v.Login != nil {
		v.Reporter = a.reporter(r, handle)
	}
	respond(w, v, err)
}

// reporter is the person signed in with a portal's own login (cookie).
func (a *api) reporter(r *http.Request, handle string) *core.Reporter {
	c, err := r.Cookie(core.ReporterCookie)
	if err != nil {
		return nil
	}
	return a.core.ReporterFor(r.Context(), handle, c.Value)
}

const reporterStateCookie = "phx_fblogin"

// reporterLogin sends the browser to the portal's identity provider.
func (a *api) reporterLogin(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")
	u, state, err := a.core.ReporterLoginStart(r.Context(), handle)
	if err != nil {
		http.Redirect(w, r, "/feedback/"+handle+"?login=failed", http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: reporterStateCookie, Value: state, Path: "/api/v1/feedback/login", MaxAge: 600, HttpOnly: true, Secure: a.auth.SecureCookies, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, u, http.StatusFound)
}

// reporterCallback finishes the sign-in and returns to the portal.
func (a *api) reporterCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cookieState := ""
	if c, err := r.Cookie(reporterStateCookie); err == nil {
		cookieState = c.Value
	}
	http.SetCookie(w, &http.Cookie{Name: reporterStateCookie, Value: "", Path: "/api/v1/feedback/login", MaxAge: -1, HttpOnly: true, Secure: a.auth.SecureCookies, SameSite: http.SameSiteLaxMode})
	handle, session, err := a.core.ReporterLoginCallback(r.Context(), q.Get("state"), cookieState, q.Get("code"), q.Get("error"))
	if handle == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	if err != nil {
		http.Redirect(w, r, "/feedback/"+handle+"?login=failed", http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: core.ReporterCookie, Value: session, Path: "/api/v1/feedback/" + handle, MaxAge: 7 * 24 * 3600, HttpOnly: true, Secure: a.auth.SecureCookies, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/feedback/"+handle, http.StatusFound)
}

// reporterLogout ends the sign-in with the portal's login.
func (a *api) reporterLogout(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")
	if c, err := r.Cookie(core.ReporterCookie); err == nil {
		a.core.ReporterLogout(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: core.ReporterCookie, Value: "", Path: "/api/v1/feedback/" + handle, MaxAge: -1, HttpOnly: true, Secure: a.auth.SecureCookies, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

// submitFeedback takes a report as JSON or as multipart form (field
// "report" with the JSON, files in "file"). Signed-in people, anonymous
// visitors (with the bot check), apps with the public key (with the bot
// check) and backends with the server key (header X-Phoedox-Feedback-Key).
func (a *api) submitFeedback(w http.ResponseWriter, r *http.Request) {
	handle := chi.URLParam(r, "handle")
	if !a.feedbackIP.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many reports. Please wait a while."))
		return
	}
	var in struct {
		core.ReportInput
		BotToken string `json:"botToken"`
	}
	var files []core.ReportFile
	ctype, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ctype == "multipart/form-data" {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<30)
		mr, err := r.MultipartReader()
		if err != nil {
			writeErr(w, &core.InvalidError{Key: "validation.json", Msg: "invalid form"})
			return
		}
		// Parts are read in order: the report first, then the files, which
		// are spooled as they arrive.
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			switch part.FormName() {
			case "report":
				if err := json.NewDecoder(http.MaxBytesReader(w, part, 1<<20)).Decode(&in); err != nil {
					writeErr(w, &core.InvalidError{Key: "validation.json", Msg: "invalid JSON"})
					return
				}
			case "file":
				sp, err := a.core.SpoolUpload(part)
				if err != nil {
					writeErr(w, err)
					return
				}
				defer sp.Remove()
				files = append(files, core.ReportFile{Name: part.FileName(), R: sp})
			}
		}
	} else if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeErr(w, &core.InvalidError{Key: "validation.json", Msg: "invalid JSON"})
		return
	}
	intake := core.Intake{Via: "form"}
	var actor *core.Actor
	if key := r.Header.Get("X-Phoedox-Feedback-Key"); key != "" {
		_, via, err := a.core.PortalByKey(r.Context(), handle, key)
		if err != nil {
			writeJSON(w, http.StatusForbidden, errBody("forbidden", "The key is not valid for this portal."))
			return
		}
		intake.Via = via
	} else if act, viaCookie, err := a.auth.FromRequest(r, "token"); err == nil {
		if viaCookie && r.Header.Get("X-Phoedox") != "1" {
			writeJSON(w, http.StatusForbidden, errBody("csrf", "Missing X-Phoedox header."))
			return
		}
		actor = &act
	}
	if r.URL.Query().Get("via") == "widget" && intake.Via == "form" {
		intake.Via = "widget"
	}
	if intake.Via == "form" || intake.Via == "widget" {
		intake.Reporter = a.reporter(r, handle)
	}
	// Anonymous reports and reports with the public key pass the bot check.
	if (actor == nil && intake.Reporter == nil && intake.Via != "server_key") && a.core.Bot != nil && a.core.Bot.Enabled(r.Context(), botcheck.PlaceFeedback) {
		if !a.botCheck(w, r, botcheck.PlaceFeedback, in.BotToken) {
			return
		}
		intake.Check = a.core.Bot.Config(r.Context()).Places[botcheck.PlaceFeedback]
	}
	res, err := a.core.SubmitFeedback(r.Context(), actor, handle, in.ReportInput, files, intake)
	respondStatus(w, http.StatusCreated, res, err)
}

func (a *api) feedbackStatus(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.FeedbackStatus(r.Context(), chi.URLParam(r, "token"))
	respond(w, v, err)
}

func (a *api) deleteFeedbackStatus(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, a.core.DeleteFeedbackByToken(r.Context(), chi.URLParam(r, "token")))
}

func (a *api) exportFeedbackStatus(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.ExportFeedbackByToken(r.Context(), chi.URLParam(r, "token"))
	if err == nil {
		w.Header().Set("Content-Disposition", `attachment; filename="feedback-report.json"`)
	}
	respond(w, v, err)
}

func (a *api) myFeedback(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.MyFeedback(r.Context(), actorOf(r))
	respond(w, v, err)
}

// --- moderators ---------------------------------------------------------------

func (a *api) feedbackPortals(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.FeedbackPortals(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

func (a *api) saveFeedbackPortal(w http.ResponseWriter, r *http.Request) {
	var in core.PortalInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SaveFeedbackPortal(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, v, err)
}

func (a *api) deleteFeedbackPortal(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, a.core.DeleteFeedbackPortal(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}

func (a *api) rotateFeedbackKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind string `json:"kind"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.RotateFeedbackKey(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.Kind)
	respond(w, v, err)
}

func (a *api) setFeedbackModerators(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Emails []string `json:"emails"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetFeedbackModerators(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.Emails)
	respond(w, v, err)
}

func (a *api) feedbackTopics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	prio, _ := strconv.Atoi(q.Get("priority"))
	v, err := a.core.FeedbackTopics(r.Context(), actorOf(r), chi.URLParam(r, "p"), core.TopicQuery{Portal: q.Get("portal"), State: q.Get("state"), Category: q.Get("category"), Priority: prio, Q: q.Get("q")})
	respond(w, v, err)
}

func (a *api) feedbackTopic(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.FeedbackTopic(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, v, err)
}

func (a *api) updateFeedbackTopic(w http.ResponseWriter, r *http.Request) {
	var in core.TopicPatch
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.UpdateFeedbackTopic(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, v, err)
}

func (a *api) mergeFeedbackTopic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From string `json:"from"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.MergeFeedbackTopics(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.From)
	respond(w, v, err)
}

func (a *api) moveFeedbackReport(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Topic string `json:"topic"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.MoveFeedbackReport(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.Topic)
	respond(w, v, err)
}

func (a *api) feedbackToBoard(w http.ResponseWriter, r *http.Request) {
	var in core.TicketInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.FeedbackToBoard(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, v, err)
}

func (a *api) feedbackAttachment(w http.ResponseWriter, r *http.Request) {
	f, rc, err := a.core.FeedbackAttachment(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rc.Close()
	h := w.Header()
	base, _, _ := mime.ParseMediaType(f.Mime)
	h.Set("Content-Type", f.Mime)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; media-src 'self'; sandbox")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Cache-Control", "private, max-age=3600")
	disp := "attachment"
	if (strings.HasPrefix(base, "image/") && base != "image/svg+xml") || strings.HasPrefix(base, "video/") || base == "text/plain" {
		disp = "inline"
	}
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": f.Name}))
	http.ServeContent(w, r, "", time.Time{}, rc)
}

func (a *api) setFeedbackQuota(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MB *int `json:"mb"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, nil, a.core.SetFeedbackQuota(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.MB))
}

type answerDraftIn struct {
	Lang string `json:"lang"`
}

func (a *api) feedbackAnswerDraft(w http.ResponseWriter, r *http.Request) {
	var in answerDraftIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.DraftFeedbackAnswer(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.Lang)
	respond(w, v, err)
}

func (a *api) entityFeedback(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.EntityFeedbackTopics(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "e"))
	respond(w, v, err)
}

func (a *api) taskFeedbackSeen(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, a.core.SeenTaskFeedback(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}

type feedbackReplyIn struct {
	Body string `json:"body"`
}

func (a *api) replyFeedbackStatus(w http.ResponseWriter, r *http.Request) {
	if !a.feedbackIP.allow(clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many answers. Please wait a while."))
		return
	}
	var in feedbackReplyIn
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.ReplyFeedbackByToken(r.Context(), chi.URLParam(r, "token"), in.Body)
	respond(w, v, err)
}

// --- external trackers ---------------------------------------------------------

func (a *api) feedbackTrackers(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.FeedbackTrackers(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

func (a *api) saveFeedbackTracker(w http.ResponseWriter, r *http.Request) {
	var in core.TrackerInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SaveFeedbackTracker(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in)
	respond(w, v, err)
}

func (a *api) deleteFeedbackTracker(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, a.core.DeleteFeedbackTracker(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id")))
}

func (a *api) forwardFeedbackTopic(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Tracker string `json:"tracker"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.ForwardFeedbackTopic(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.Tracker)
	respond(w, v, err)
}

func (a *api) refreshFeedbackForwards(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.RefreshFeedbackForwards(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, v, err)
}

// --- more feedback storage ------------------------------------------------------

func (a *api) feedbackStorage(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.ProjectFeedbackStorage(r.Context(), actorOf(r), chi.URLParam(r, "p"))
	respond(w, v, err)
}

func (a *api) buyFeedbackStorage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Offer string `json:"offer"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.BuyFeedbackStorage(r.Context(), actorOf(r), chi.URLParam(r, "p"), in.Offer)
	respond(w, v, err)
}

func (a *api) feedbackStorageOffers(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.FeedbackStorageOffers(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) setFeedbackStorageOffers(w http.ResponseWriter, r *http.Request) {
	var in []core.StorageOffer
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetFeedbackStorageOffers(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

// --- own domains of portals -----------------------------------------------------

func (a *api) setPortalDomain(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Domain string `json:"domain"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SetFeedbackPortalDomain(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"), in.Domain)
	respond(w, v, err)
}

func (a *api) verifyPortalDomain(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.VerifyFeedbackPortalDomain(r.Context(), actorOf(r), chi.URLParam(r, "p"), chi.URLParam(r, "id"))
	respond(w, v, err)
}
