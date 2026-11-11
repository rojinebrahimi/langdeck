package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/rojinebrahimi/langdeck/internal/domain"
	"github.com/rojinebrahimi/langdeck/internal/notify"
)

const notifySettingKey = "notifications"

// NotifySettings returns the notification settings in effect: the ones saved from the UI
// if any, otherwise the LANGDECK_NOTIFY_* environment.
func (a *API) NotifySettings(ctx context.Context) (s notify.Settings, fromUI bool, err error) {
	raw, err := a.Store.GetSetting(ctx, notifySettingKey)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s); err != nil {
			return s, true, err
		}
		return s, true, nil
	case errors.Is(err, domain.ErrNotFound):
		return a.envNotifySettings(), false, nil
	default:
		return s, false, err
	}
}

func (a *API) envNotifySettings() notify.Settings {
	c := a.Cfg
	return notify.Settings{Events: c.NotifyEvents, SlackURL: c.NotifySlackURL, MattermostURL: c.NotifyMattermostURL,
		TeamsURL: c.NotifyTeamsURL, TelegramToken: c.NotifyTelegramToken, TelegramChatID: c.NotifyTelegramChatID}
}

// notifyView is what clients see: never a URL or token, only whether a channel is set.
type notifyView struct {
	Events          []string        `json:"events"`
	AvailableEvents []string        `json:"available_events"`
	Channels        map[string]bool `json:"channels"`
	TelegramChatID  string          `json:"telegram_chat_id"`
	Source          string          `json:"source"` // "ui" or "env"
}

func (a *API) notifyView(s notify.Settings, fromUI bool) notifyView {
	events := s.Events
	if len(events) == 0 {
		events = notify.DefaultKinds
	}
	src := "env"
	if fromUI {
		src = "ui"
	}
	return notifyView{Events: events, AvailableEvents: notify.AllKinds, Channels: s.Configured(), TelegramChatID: s.TelegramChatID, Source: src}
}

func (a *API) getNotifySettings(w http.ResponseWriter, r *http.Request) {
	s, fromUI, err := a.NotifySettings(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a.notifyView(s, fromUI))
}

func (a *API) putNotifySettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Events         *[]string `json:"events"`
		SlackURL       string    `json:"slack_url"`
		MattermostURL  string    `json:"mattermost_url"`
		TeamsURL       string    `json:"teams_url"`
		TelegramToken  string    `json:"telegram_token"`
		TelegramChatID string    `json:"telegram_chat_id"`
		Clear          []string  `json:"clear"`
	}
	if err := decode(r, &in); err != nil {
		a.fail(w, r, err)
		return
	}
	s, _, err := a.NotifySettings(r.Context())
	if err != nil {
		a.fail(w, r, err)
		return
	}
	before := s.Configured()
	// Blank fields keep what is stored, so the UI never has to hold a secret.
	set := func(dst *string, v string) {
		if v = strings.TrimSpace(v); v != "" {
			*dst = v
		}
	}
	set(&s.SlackURL, in.SlackURL)
	set(&s.MattermostURL, in.MattermostURL)
	set(&s.TeamsURL, in.TeamsURL)
	set(&s.TelegramToken, in.TelegramToken)
	set(&s.TelegramChatID, in.TelegramChatID)
	for _, ch := range in.Clear {
		if err := s.Clear(ch); err != nil {
			a.fail(w, r, invalid(err.Error()))
			return
		}
	}
	if in.Events != nil {
		s.Events = slices.Compact(slices.Sorted(slices.Values(*in.Events)))
	}
	if err := s.Validate(); err != nil {
		a.fail(w, r, invalid(err.Error()))
		return
	}
	raw, _ := json.Marshal(s)
	if err := a.Store.SetSetting(r.Context(), notifySettingKey, raw, a.Now()); err != nil {
		a.fail(w, r, err)
		return
	}
	a.Notify.Reconfigure(s.Options(notify.Options{PublicURL: a.Cfg.PublicURL, Log: a.Log}))
	after := s.Configured()
	var changed []string
	for ch, on := range after {
		if on != before[ch] || (on && (in.SlackURL != "" || in.MattermostURL != "" || in.TeamsURL != "" || in.TelegramToken != "")) {
			changed = append(changed, ch)
		}
	}
	slices.Sort(changed)
	a.audit(r, "settings.notifications.update", "settings", notifySettingKey, map[string]any{"channels": changed, "events": s.Events})
	writeJSON(w, http.StatusOK, a.notifyView(s, true))
}

func (a *API) testNotify(w http.ResponseWriter, r *http.Request) {
	if !a.Notify.Enabled() {
		writeError(w, http.StatusBadRequest, "no channel is configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res := a.Notify.SendTest(ctx, notify.Event{
		Kind: "test", Title: "LangDeck test notification",
		Text: "If you can read this, notifications from LangDeck reach this channel.",
		URL:  "/", Fields: [][2]string{{"Sent by", "settings page"}},
	})
	writeJSON(w, http.StatusOK, map[string]any{"results": res})
}
