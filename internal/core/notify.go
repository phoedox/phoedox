package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/SherClockHolmes/webpush-go"

	"github.com/phoedox/phoedox/internal/mail"
	"github.com/phoedox/phoedox/internal/store"
	"github.com/phoedox/phoedox/resources"
)

// Notifications (concept "Benachrichtigungen"): everything that is meant
// for one person lands in the app; mails go out at once, as a daily digest
// or not at all; ntfy and web push are optional. Muted kinds stay in the
// app only.

// NotificationKinds are the kinds people can mute.
var NotificationKinds = []string{"comment.mention", "comment.reply", "check.fail", "check.recover", "task.assigned", "project.summary", "draft.review", "draft.changes_requested", "draft.approved", "page.review_due", "page.required_reading", "entity.owner", "entity.review_due"}

// Note is one notification before it is stored and sent.
type Note struct {
	Kind      string
	ProjectID string
	Project   string // project name for mails
	Subject   string // id of the comment, check, task …
	Title     string // what it is about (page title, check name, task title)
	Body      string // excerpt
	Link      string // path in the app
	Actor     string // name of who caused it
	// Mail renders the mail sent at once; nil uses the generic notice mail.
	Mail func(u store.User, locale string) (subject, body string, err error)
}

func (s *Service) locale(u store.User) string {
	if u.Locale != "" {
		return u.Locale
	}
	return s.DefaultLocale
}

// headline is the localized first line of a notification (push, digest).
func (s *Service) headline(u store.User, n Note) string {
	subject, _, err := resources.Mail("notice", s.locale(u), s.noticeData(u, n))
	if err != nil {
		return n.Title
	}
	return subject
}

func (s *Service) noticeData(u store.User, n Note) map[string]any {
	return map[string]any{"Kind": n.Kind, "Name": u.Name, "Actor": n.Actor, "Title": n.Title, "Project": n.Project, "Body": n.Body, "Link": s.PublicURL + n.Link}
}

func excerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > max {
		return string([]rune(s)[:max]) + " …"
	}
	return s
}

// notify stores a notification for a person and sends it on their channels.
func (s *Service) notify(ctx context.Context, u store.User, n Note) {
	if u.Disabled || u.ID == "" {
		return
	}
	set, err := s.DB.GetNotificationSettings(ctx, u.ID)
	if err != nil {
		slog.Warn("notification settings", "user", u.ID, "err", err)
	}
	muted := slices.Contains(set.Muted, n.Kind)
	mailed := muted || set.Email == store.MailOff // nothing left for the digest
	if !muted && set.Email == store.MailInstant && s.Mail != nil && s.Mail.Enabled() && s.PublicURL != "" {
		render := n.Mail
		if render == nil {
			render = func(u store.User, locale string) (string, string, error) {
				return resources.Mail("notice", locale, s.noticeData(u, n))
			}
		}
		subject, body, err := render(u, s.locale(u))
		if err == nil {
			err = s.Mail.Send(ctx, mail.Message{To: u.Email, Subject: subject, Body: body})
		}
		if err != nil {
			slog.Warn("notification mail failed", "user", u.ID, "kind", n.Kind, "err", err)
		}
		mailed = true
	}
	stored, err := s.DB.AddNotification(ctx, store.Notification{ID: newID(), UserID: u.ID, ProjectID: n.ProjectID, Kind: n.Kind, SubjectID: n.Subject,
		Title: n.Title, Body: excerpt(n.Body, 400), Link: n.Link, ActorName: n.Actor}, mailed)
	if err != nil {
		slog.Warn("notification", "user", u.ID, "err", err)
		return
	}
	if muted {
		return
	}
	send := func() {
		pctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		s.pushOut(pctx, u, set, n, stored.ID)
	}
	if s.SyncNotify {
		send()
	} else {
		go send()
	}
}

// pushOut sends a notification to ntfy and the person's browsers.
func (s *Service) pushOut(ctx context.Context, u store.User, set store.NotificationSettings, n Note, id string) {
	head := s.headline(u, n)
	link := s.PublicURL + n.Link
	if set.NtfyURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, set.NtfyURL, strings.NewReader(excerpt(n.Body, 400)))
		if err == nil {
			req.Header.Set("Title", head)
			req.Header.Set("Tags", "phoedox")
			if s.PublicURL != "" {
				req.Header.Set("Click", link)
			}
			var res *http.Response
			if res, err = s.RequestPolicy.Client().Do(req); err == nil {
				res.Body.Close()
				if res.StatusCode >= 300 {
					err = errors.New(res.Status)
				}
			}
		}
		if err != nil {
			slog.Warn("ntfy failed", "user", u.ID, "err", err)
		}
	}
	if set.MatrixHomeserver != "" && set.MatrixRoom != "" && set.MatrixToken != "" {
		if err := s.sendMatrix(ctx, set, head, excerpt(n.Body, 400), link); err != nil {
			slog.Warn("matrix failed", "user", u.ID, "err", err)
		}
	}
	subs, err := s.DB.PushSubscriptions(ctx, u.ID)
	if err != nil || len(subs) == 0 {
		return
	}
	pub, priv, err := s.vapidKeys(ctx)
	if err != nil {
		slog.Warn("web push keys", "err", err)
		return
	}
	payload, _ := json.Marshal(map[string]string{"title": head, "body": excerpt(n.Body, 200), "url": n.Link, "tag": id})
	subscriber := s.PublicURL
	if subscriber == "" {
		subscriber = "https://phoedox.com"
	}
	for _, sub := range subs {
		res, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{Endpoint: sub.Endpoint, Keys: webpush.Keys{Auth: sub.Auth, P256dh: sub.P256dh}},
			&webpush.Options{Subscriber: subscriber, VAPIDPublicKey: pub, VAPIDPrivateKey: priv, TTL: 24 * 3600, HTTPClient: s.RequestPolicy.Client()})
		if err != nil {
			slog.Warn("web push failed", "user", u.ID, "err", err)
			continue
		}
		res.Body.Close()
		if res.StatusCode == http.StatusGone || res.StatusCode == http.StatusNotFound {
			_ = s.DB.DeletePushSubscription(ctx, "", sub.Endpoint) // the browser unsubscribed
		}
	}
}

const (
	vapidPublicKey  = "push.vapid_public"
	vapidPrivateKey = "push.vapid_private"
)

// vapidKeys returns the key pair web push messages are signed with; the
// private key is created on first use and stored sealed.
func (s *Service) vapidKeys(ctx context.Context) (pub, priv string, err error) {
	if s.Box == nil {
		return "", "", errors.New("web push needs the master key")
	}
	pub, _ = s.DB.Setting(ctx, vapidPublicKey, "")
	sealed, _ := s.DB.Setting(ctx, vapidPrivateKey, "")
	if pub != "" && sealed != "" {
		b, err := s.Box.Open(sealed, vapidPrivateKey)
		return pub, string(b), err
	}
	s.vapidMu.Lock()
	defer s.vapidMu.Unlock()
	if pub, _ = s.DB.Setting(ctx, vapidPublicKey, ""); pub != "" {
		return s.vapidKeys(ctx)
	}
	priv, pub, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", err
	}
	if sealed, err = s.Box.Seal([]byte(priv), vapidPrivateKey); err != nil {
		return "", "", err
	}
	if err := s.DB.SetSetting(ctx, vapidPrivateKey, sealed, "system"); err != nil {
		return "", "", err
	}
	return pub, priv, s.DB.SetSetting(ctx, vapidPublicKey, pub, "system")
}

// NotificationList is what the bell in the app shows.
type NotificationList struct {
	Items  []store.Notification `json:"items"`
	Unread int                  `json:"unread"`
}

// Notifications lists the newest notifications of the actor.
func (s *Service) Notifications(ctx context.Context, a Actor, unread bool, limit int) (NotificationList, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	items, err := s.DB.UserNotifications(ctx, a.User.ID, unread, limit)
	if err != nil {
		return NotificationList{}, err
	}
	n, err := s.DB.UnreadNotifications(ctx, a.User.ID)
	return NotificationList{Items: items, Unread: n}, err
}

// MarkNotificationsRead marks notifications (none = all) of the actor as read.
func (s *Service) MarkNotificationsRead(ctx context.Context, a Actor, ids []string) error {
	return s.DB.MarkNotificationsRead(ctx, a.User.ID, ids)
}

// NotificationSettingsView adds what the browser needs for web push.
type NotificationSettingsView struct {
	store.NotificationSettings
	// MatrixConnected: an access token is stored (it is never shown).
	MatrixConnected bool     `json:"matrixConnected"`
	Kinds           []string `json:"kinds"`
	PushKey         string   `json:"pushKey,omitempty"`
	Subscriptions   []string `json:"subscriptions"`
	MailEnabled     bool     `json:"mailEnabled"`
}

// NotificationSettings returns the actor's channels.
func (s *Service) NotificationSettings(ctx context.Context, a Actor) (NotificationSettingsView, error) {
	set, err := s.DB.GetNotificationSettings(ctx, a.User.ID)
	if err != nil {
		return NotificationSettingsView{}, err
	}
	v := NotificationSettingsView{NotificationSettings: set, Kinds: NotificationKinds, Subscriptions: []string{},
		MailEnabled: s.Mail != nil && s.Mail.Enabled(), MatrixConnected: set.MatrixToken != ""}
	if pub, _, err := s.vapidKeys(ctx); err == nil {
		v.PushKey = pub
	}
	subs, _ := s.DB.PushSubscriptions(ctx, a.User.ID)
	for _, sub := range subs {
		v.Subscriptions = append(v.Subscriptions, sub.Endpoint)
	}
	return v, nil
}

// NotificationSettingsInput changes the channels. The Matrix access token is
// write-only: nil keeps the stored one, "" removes it.
type NotificationSettingsInput struct {
	store.NotificationSettings
	MatrixAccessToken *string `json:"matrixToken,omitempty"`
}

// SaveNotificationSettings changes the actor's channels.
func (s *Service) SaveNotificationSettings(ctx context.Context, a Actor, input NotificationSettingsInput) (NotificationSettingsView, error) {
	in := input.NotificationSettings
	cur, err := s.DB.GetNotificationSettings(ctx, a.User.ID)
	if err != nil {
		return NotificationSettingsView{}, err
	}
	in.MatrixToken = cur.MatrixToken
	in.MatrixHomeserver, in.MatrixRoom = strings.TrimRight(strings.TrimSpace(in.MatrixHomeserver), "/"), strings.TrimSpace(in.MatrixRoom)
	if input.MatrixAccessToken != nil {
		in.MatrixToken = ""
		if t := strings.TrimSpace(*input.MatrixAccessToken); t != "" {
			if s.Box == nil {
				return NotificationSettingsView{}, invalid("validation.notify_matrix_key", nil, "Matrix needs the master key to store the token")
			}
			if in.MatrixToken, err = s.Box.Seal([]byte(t), matrixContext(a.User.ID)); err != nil {
				return NotificationSettingsView{}, err
			}
		}
	}
	if in.MatrixHomeserver != "" || in.MatrixRoom != "" {
		u, err := url.Parse(in.MatrixHomeserver)
		if err != nil || u.Scheme != "https" || u.Host == "" || !matrixRoomRe.MatchString(in.MatrixRoom) {
			return NotificationSettingsView{}, invalid("validation.notify_matrix", nil, "Matrix needs an https homeserver and a room id like !abc:example.org")
		}
	}
	if !slices.Contains([]string{store.MailInstant, store.MailDaily, store.MailOff}, in.Email) {
		return NotificationSettingsView{}, invalid("validation.notify_email", nil, "unknown mail mode %q", in.Email)
	}
	in.NtfyURL = strings.TrimSpace(in.NtfyURL)
	if in.NtfyURL != "" {
		u, err := url.Parse(in.NtfyURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.Trim(u.Path, "/") == "" {
			return NotificationSettingsView{}, invalid("validation.notify_ntfy", nil, "the ntfy address needs a topic, e.g. https://ntfy.sh/my-topic")
		}
	}
	muted := []string{}
	for _, k := range in.Muted {
		if slices.Contains(NotificationKinds, k) && !slices.Contains(muted, k) {
			muted = append(muted, k)
		}
	}
	in.Muted, in.UserID = muted, a.User.ID
	if err := s.DB.SaveNotificationSettings(ctx, in); err != nil {
		return NotificationSettingsView{}, err
	}
	return s.NotificationSettings(ctx, a)
}

// PushSubscriptionInput is what the browser's PushManager returns.
type PushSubscriptionInput struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// AddPushSubscription registers a browser of the actor for web push.
func (s *Service) AddPushSubscription(ctx context.Context, a Actor, in PushSubscriptionInput) error {
	u, err := url.Parse(in.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || in.Keys.P256dh == "" || in.Keys.Auth == "" || len(in.Endpoint) > 2000 {
		return invalid("validation.push_subscription", nil, "invalid push subscription")
	}
	return s.DB.AddPushSubscription(ctx, store.PushSubscription{ID: newID(), UserID: a.User.ID, Endpoint: in.Endpoint, P256dh: in.Keys.P256dh, Auth: in.Keys.Auth})
}

// DeletePushSubscription unregisters a browser of the actor.
func (s *Service) DeletePushSubscription(ctx context.Context, a Actor, endpoint string) error {
	return s.DB.DeletePushSubscription(ctx, a.User.ID, endpoint)
}

// TestNotification sends a test message on all of the actor's channels.
func (s *Service) TestNotification(ctx context.Context, a Actor) error {
	s.notify(ctx, a.User, Note{Kind: "test", Title: "Phoedox", Link: "/settings/notifications"})
	return nil
}

// SendDigests mails the daily digest to everyone who chose it and has
// unread news; at most once in 23 hours, not before 6 o'clock UTC.
func (s *Service) SendDigests(ctx context.Context, now time.Time) {
	if s.Mail == nil || !s.Mail.Enabled() || s.PublicURL == "" || now.UTC().Hour() < 6 {
		return
	}
	users, err := s.DB.DigestUsers(ctx)
	if err != nil {
		slog.Warn("digests", "err", err)
		return
	}
	for _, set := range users {
		if !set.LastDigest.IsZero() && now.Sub(set.LastDigest.Time) < 23*time.Hour {
			continue
		}
		tctx := store.WithTenant(ctx, set.TenantID)
		s.sendDigest(tctx, set.UserID, now)
	}
}

func (s *Service) sendDigest(ctx context.Context, userID string, now time.Time) {
	u, err := s.DB.UserByID(ctx, userID)
	if err != nil || u.Disabled {
		return
	}
	pending, err := s.DB.DigestPending(ctx, userID)
	if err != nil || len(pending) == 0 {
		return
	}
	type item struct{ Headline, Body, Link string }
	items := make([]item, 0, len(pending))
	ids := make([]string, 0, len(pending))
	for _, n := range pending {
		items = append(items, item{Headline: s.headline(u, Note{Kind: n.Kind, Title: n.Title, Actor: n.ActorName}), Body: excerpt(n.Body, 200), Link: s.PublicURL + n.Link})
		ids = append(ids, n.ID)
	}
	subject, body, err := resources.Mail("digest", s.locale(u), map[string]any{"Name": u.Name, "Items": items})
	if err == nil {
		err = s.Mail.Send(ctx, mail.Message{To: u.Email, Subject: subject, Body: body})
	}
	if err != nil {
		slog.Warn("digest mail failed", "user", u.ID, "err", err)
		return
	}
	_ = s.DB.MarkDigestSent(ctx, userID, ids, store.Time{Time: now})
}

var matrixRoomRe = regexp.MustCompile(`^![A-Za-z0-9._=\-/+]+:[A-Za-z0-9.\-]+(:\d+)?$`)

func matrixContext(userID string) string { return "notify.matrix:" + userID }

// sendMatrix posts a message into the person's Matrix room with their own
// access token (sealed at rest).
func (s *Service) sendMatrix(ctx context.Context, set store.NotificationSettings, head, body, link string) error {
	if s.Box == nil {
		return errors.New("no master key")
	}
	token, err := s.Box.Open(set.MatrixToken, matrixContext(set.UserID))
	if err != nil {
		return err
	}
	text := head
	if body != "" {
		text += "\n" + body
	}
	htmlBody := "<b>" + html.EscapeString(head) + "</b>"
	if body != "" {
		htmlBody += "<br>" + html.EscapeString(body)
	}
	if s.PublicURL != "" {
		text += "\n" + link
		htmlBody += `<br><a href="` + html.EscapeString(link) + `">` + html.EscapeString(link) + "</a>"
	}
	msg, _ := json.Marshal(map[string]string{"msgtype": "m.notice", "body": text, "format": "org.matrix.custom.html", "formatted_body": htmlBody})
	endpoint := set.MatrixHomeserver + "/_matrix/client/v3/rooms/" + url.PathEscape(set.MatrixRoom) + "/send/m.room.message/" + newID()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(msg))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Content-Type", "application/json")
	res, err := s.RequestPolicy.Client().Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 300 {
		return errors.New(res.Status)
	}
	return nil
}
