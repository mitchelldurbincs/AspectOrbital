package hubnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	notifyURL  string
	authToken  string
	httpClient *http.Client
}

type NotifyRequest struct {
	Version               int             `json:"version"`
	TargetChannel         string          `json:"targetChannel"`
	CallbackURL           string          `json:"callbackUrl,omitempty"`
	Service               string          `json:"service"`
	Event                 string          `json:"event"`
	Severity              string          `json:"severity"`
	Title                 string          `json:"title"`
	Summary               string          `json:"summary"`
	URL                   string          `json:"url,omitempty"`
	Fields                []NotifyField   `json:"fields"`
	Actions               []NotifyAction  `json:"actions"`
	AllowedMentions       AllowedMentions `json:"allowedMentions"`
	Visibility            string          `json:"visibility"`
	SuppressNotifications bool            `json:"suppressNotifications"`
	OccurredAt            time.Time       `json:"occurredAt"`
}

type NotifyField struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Group  string `json:"group"`
	Order  int    `json:"order"`
	Inline bool   `json:"inline"`
}

type AllowedMentions struct {
	Parse       []string `json:"parse"`
	Users       []string `json:"users"`
	Roles       []string `json:"roles"`
	RepliedUser bool     `json:"repliedUser"`
}

type NotifyAction struct {
	ID    string `json:"id,omitempty"`
	Label string `json:"label"`
	Style string `json:"style"`
	URL   string `json:"url,omitempty"`
}

type ActionCallbackRequest struct {
	Version     int                   `json:"version"`
	Service     string                `json:"service"`
	Event       string                `json:"event"`
	Severity    string                `json:"severity"`
	Action      ActionCallbackAction  `json:"action"`
	Context     ActionCallbackContext `json:"context"`
	SourceTitle string                `json:"sourceTitle"`
	SourceURL   string                `json:"sourceUrl,omitempty"`
}

type ActionCallbackAction struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Style string `json:"style"`
}

type ActionCallbackContext struct {
	DiscordUserID string `json:"discordUserId"`
	GuildID       string `json:"guildId,omitempty"`
	ChannelID     string `json:"channelId,omitempty"`
	MessageID     string `json:"messageId,omitempty"`
	InteractionID string `json:"interactionId,omitempty"`
}

type ActionCallbackResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

const (
	Version2             = 2
	SeverityInfo         = "info"
	SeverityWarning      = "warning"
	SeverityCritical     = "critical"
	VisibilityPublic     = "public"
	VisibilityEphemeral  = "ephemeral"
	FieldGroupContext    = "Context"
	FieldGroupMetrics    = "Metrics"
	FieldGroupTiming     = "Timing"
	FieldGroupLinks      = "Links"
	ActionStylePrimary   = "primary"
	ActionStyleSecondary = "secondary"
	ActionStyleSuccess   = "success"
	ActionStyleDanger    = "danger"
	ActionStyleLink      = "link"
)

func CanonicalTitle(service string, event string) string {
	serviceName := strings.ToUpper(strings.TrimSpace(service))
	eventName := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(event), "-", " "))
	eventName = strings.ReplaceAll(eventName, "_", " ")
	return "[" + serviceName + "] " + eventName
}

func NewClient(notifyURL string, authToken string, httpClient *http.Client) *Client {
	return &Client{
		notifyURL:  notifyURL,
		authToken:  authToken,
		httpClient: httpClient,
	}
}

func (c *Client) Notify(ctx context.Context, payload NotifyRequest) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.notifyURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.authToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return fmt.Errorf("discord-hub notify failed (%s): %s", resp.Status, strings.TrimSpace(string(responseBody)))
	}

	return nil
}
