package notify

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// AllKinds lists every event kind a user can subscribe to.
var AllKinds = []string{KindApproval, KindRunFailed, KindAgentOffline, KindAgentOnline}

// Channel names, as used in settings and API responses.
const (
	ChSlack      = "slack"
	ChMattermost = "mattermost"
	ChTeams      = "teams"
	ChTelegram   = "telegram"
)

// Settings is the persisted notification configuration. It holds secrets (webhook URLs
// and the bot token), so it must never be returned to clients as is.
type Settings struct {
	Events         []string `json:"events"`
	SlackURL       string   `json:"slack_url,omitempty"`
	MattermostURL  string   `json:"mattermost_url,omitempty"`
	TeamsURL       string   `json:"teams_url,omitempty"`
	TelegramToken  string   `json:"telegram_token,omitempty"`
	TelegramChatID string   `json:"telegram_chat_id,omitempty"`
}

// Options converts settings into dispatcher options.
func (s Settings) Options(base Options) Options {
	base.SlackURL, base.MattermostURL, base.TeamsURL = s.SlackURL, s.MattermostURL, s.TeamsURL
	base.TelegramToken, base.TelegramChat, base.Events = s.TelegramToken, s.TelegramChatID, s.Events
	return base
}

// Configured reports which channels have a destination.
func (s Settings) Configured() map[string]bool {
	return map[string]bool{
		ChSlack: s.SlackURL != "", ChMattermost: s.MattermostURL != "", ChTeams: s.TeamsURL != "",
		ChTelegram: s.TelegramToken != "" && s.TelegramChatID != "",
	}
}

// Clear removes a channel's destination.
func (s *Settings) Clear(channel string) error {
	switch channel {
	case ChSlack:
		s.SlackURL = ""
	case ChMattermost:
		s.MattermostURL = ""
	case ChTeams:
		s.TeamsURL = ""
	case ChTelegram:
		s.TelegramToken, s.TelegramChatID = "", ""
	default:
		return fmt.Errorf("unknown channel %q", channel)
	}
	return nil
}

// Validate checks every field. Webhook URLs must be absolute https URLs (http is allowed
// only for localhost, which is handy for development).
func (s Settings) Validate() error {
	var errs []error
	for name, raw := range map[string]string{ChSlack: s.SlackURL, ChMattermost: s.MattermostURL, ChTeams: s.TeamsURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		switch {
		case err != nil || u.Host == "":
			errs = append(errs, fmt.Errorf("%s: not a valid URL", name))
		case u.Scheme == "https":
		case u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"):
		default:
			errs = append(errs, fmt.Errorf("%s: URL must use https", name))
		}
	}
	if (s.TelegramToken == "") != (s.TelegramChatID == "") {
		errs = append(errs, errors.New("telegram: bot token and chat id must be set together"))
	}
	if strings.ContainsAny(s.TelegramToken, " \t\r\n/") {
		errs = append(errs, errors.New("telegram: bot token looks malformed"))
	}
	for _, k := range s.Events {
		ok := false
		for _, a := range AllKinds {
			ok = ok || a == k
		}
		if !ok {
			errs = append(errs, fmt.Errorf("unknown event %q", k))
		}
	}
	return errors.Join(errs...)
}
