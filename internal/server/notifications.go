package server

import (
	"net/http"
	"strconv"

	"github.com/phoedox/phoedox/internal/core"
)

func (a *api) listNotifications(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := a.core.Notifications(r.Context(), actorOf(r), r.URL.Query().Get("unread") == "1", limit)
	respond(w, list, err)
}

func (a *api) readNotifications(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, nil, a.core.MarkNotificationsRead(r.Context(), actorOf(r), in.IDs))
}

func (a *api) notificationSettings(w http.ResponseWriter, r *http.Request) {
	v, err := a.core.NotificationSettings(r.Context(), actorOf(r))
	respond(w, v, err)
}

func (a *api) saveNotificationSettings(w http.ResponseWriter, r *http.Request) {
	var in core.NotificationSettingsInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	v, err := a.core.SaveNotificationSettings(r.Context(), actorOf(r), in)
	respond(w, v, err)
}

func (a *api) addPushSubscription(w http.ResponseWriter, r *http.Request) {
	var in core.PushSubscriptionInput
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, nil, a.core.AddPushSubscription(r.Context(), actorOf(r), in))
}

func (a *api) deletePushSubscription(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	respond(w, nil, a.core.DeletePushSubscription(r.Context(), actorOf(r), in.Endpoint))
}

func (a *api) testNotification(w http.ResponseWriter, r *http.Request) {
	if !a.runLimiter.allow(actorOf(r).User.ID) {
		writeJSON(w, http.StatusTooManyRequests, errBody("rate_limited", "Too many requests. Please wait a minute."))
		return
	}
	respond(w, nil, a.core.TestNotification(r.Context(), actorOf(r)))
}
