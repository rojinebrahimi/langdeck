package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	notifySent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "langdeck_notifications_total",
		Help: "Notification deliveries by channel, event kind and outcome.",
	}, []string{"channel", "event"})
	notifyDropped = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "langdeck_notifications_dropped_total",
		Help: "Notifications dropped because the delivery queue was full.",
	}, []string{"event"})
)

// chatSender posts to a Slack or Mattermost incoming webhook.
type chatSender struct {
	name      string
	url       string
	client    *http.Client
	publicURL string
}

func (s *chatSender) Name() string { return s.name }

func (s *chatSender) Send(ctx context.Context, e Event) error {
	e.URL = absolute(s.publicURL, e.URL)
	var body string
	if s.name == "slack" {
		body = slackText(e)
	} else {
		body = mattermostText(e)
	}
	return postJSON(ctx, s.client, s.url, map[string]any{"text": body, "mrkdwn": s.name == "slack"})
}

// teamsSender posts a MessageCard to a Teams incoming webhook or workflow URL.
type teamsSender struct {
	url       string
	client    *http.Client
	publicURL string
}

func (s *teamsSender) Name() string { return "teams" }

func (s *teamsSender) Send(ctx context.Context, e Event) error {
	card := map[string]any{
		"@type":      "MessageCard",
		"@context":   "https://schema.org/extensions",
		"summary":    e.Title,
		"themeColor": "9333EA",
		"title":      e.Title,
		"text":       teamsText(e),
	}
	if link := s.link(e); link != "" {
		card["potentialAction"] = []map[string]any{{
			"@type":   "OpenUri",
			"name":    "Open in LangDeck",
			"targets": []map[string]any{{"os": "default", "uri": link}},
		}}
	}
	return postJSON(ctx, s.client, s.url, card)
}

func (s *teamsSender) link(e Event) string { return absolute(s.publicURL, e.URL) }

// telegramSender talks to the Bot API directly; no webhook setup on the Telegram side.
type telegramSender struct {
	token     string
	chat      string
	client    *http.Client
	publicURL string
}

func (s *telegramSender) Name() string { return "telegram" }

func (s *telegramSender) Send(ctx context.Context, e Event) error {
	form := url.Values{}
	form.Set("chat_id", s.chat)
	form.Set("text", telegramText(e, s.publicURL))
	form.Set("parse_mode", "HTML")
	form.Set("disable_web_page_preview", "true")
	endpoint := "https://api.telegram.org/bot" + s.token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return do(s.client, req)
}

func postJSON(ctx context.Context, c *http.Client, endpoint string, payload any) error {
	buf, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return do(c, req)
}

// do performs the request and classifies the response: 2xx succeeds, 4xx (except 429)
// will never succeed and is reported as permanent, everything else is retried.
func do(c *http.Client, req *http.Request) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	err = fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(snippet)))
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
		return &permanentError{err: err}
	}
	return err
}

func slackText(e Event) string {
	var b strings.Builder
	b.WriteString("*" + escapeSlack(e.Title) + "*")
	if e.Text != "" {
		b.WriteString("\n" + escapeSlack(e.Text))
	}
	b.WriteString(fieldsText(e.Fields, "*%s:* %s", "• "))
	if e.URL != "" {
		b.WriteString("\n<" + e.URL + "|Open in LangDeck>")
	}
	return b.String()
}

func mattermostText(e Event) string {
	var b strings.Builder
	b.WriteString("**" + e.Title + "**")
	if e.Text != "" {
		b.WriteString("\n" + e.Text)
	}
	b.WriteString(fieldsText(e.Fields, "**%s:** %s", "- "))
	if e.URL != "" {
		b.WriteString("\n[Open in LangDeck](" + e.URL + ")")
	}
	return b.String()
}

func teamsText(e Event) string {
	var b strings.Builder
	if e.Text != "" {
		b.WriteString(e.Text)
	}
	b.WriteString(fieldsText(e.Fields, "**%s:** %s", "<br>"))
	return b.String()
}

func telegramText(e Event, publicURL string) string {
	var b strings.Builder
	b.WriteString("<b>" + html.EscapeString(e.Title) + "</b>")
	if e.Text != "" {
		b.WriteString("\n" + html.EscapeString(e.Text))
	}
	for _, kv := range e.Fields {
		if kv[1] == "" {
			continue
		}
		b.WriteString("\n<b>" + html.EscapeString(kv[0]) + ":</b> " + html.EscapeString(kv[1]))
	}
	if link := absolute(publicURL, e.URL); link != "" {
		b.WriteString("\n" + `<a href="` + html.EscapeString(link) + `">Open in LangDeck</a>`)
	}
	return b.String()
}

// fieldsText renders key/value pairs in a transport-specific shape.
func fieldsText(fields [][2]string, format, prefix string) string {
	var b strings.Builder
	for _, kv := range fields {
		if kv[1] == "" {
			continue
		}
		b.WriteString("\n" + prefix + fmt.Sprintf(format, kv[0], kv[1]))
	}
	return b.String()
}

// escapeSlack neutralises the three characters that would break Slack mrkdwn.
func escapeSlack(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func absolute(base, path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(base, "/") + path
}
