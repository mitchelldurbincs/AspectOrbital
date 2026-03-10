package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"personal-infrastructure/pkg/httpjson"
	"personal-infrastructure/pkg/hubnotify"
)

type notifyPayload = hubnotify.NotifyRequest

type hubHandler struct {
	log             *log.Logger
	session         discordMessageSender
	channelNameToID map[string]string
	actionCallbacks *actionCallbackRegistry
	notifyAuthToken string
}

type discordMessageSender interface {
	ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error)
}

const (
	notifyDispatchTimeout   = 8 * time.Second
	notifyTitlePattern      = `^\[[A-Z0-9_-]{1,32}\] [A-Z0-9 _-]{1,80}$`
	componentIDPrefix       = "ao2"
	componentIDMaxLen       = 100
	maxNotifySummaryLen     = 1024
	maxNotifyFieldKeyLen    = 64
	maxNotifyFieldValLen    = 1024
	maxNotifyFieldCount     = 25
	maxNotifyActionCount    = 5
	maxNotifyMentionCount   = 100
	notifyActionCallbackTTL = 7 * 24 * time.Hour
	notifyFooterSeparator   = " :: "
)

var (
	notifyTitleRegex     = regexp.MustCompile(notifyTitlePattern)
	notifySnowflakeRegex = regexp.MustCompile(`^[0-9]{17,20}$`)
	notifySeverityColors = map[string]int{
		hubnotify.SeverityInfo:     0x1F6FEB,
		hubnotify.SeverityWarning:  0xD29922,
		hubnotify.SeverityCritical: 0xCF222E,
	}
	notifyGroupRank = map[string]int{
		hubnotify.FieldGroupContext: 1,
		hubnotify.FieldGroupMetrics: 2,
		hubnotify.FieldGroupTiming:  3,
		hubnotify.FieldGroupLinks:   4,
	}
	allowedMentionTypes = map[string]discordgo.AllowedMentionType{
		"users":    discordgo.AllowedMentionTypeUsers,
		"roles":    discordgo.AllowedMentionTypeRoles,
		"everyone": discordgo.AllowedMentionTypeEveryone,
	}
	allowedActionStyles = map[string]discordgo.ButtonStyle{
		hubnotify.ActionStylePrimary:   discordgo.PrimaryButton,
		hubnotify.ActionStyleSecondary: discordgo.SecondaryButton,
		hubnotify.ActionStyleSuccess:   discordgo.SuccessButton,
		hubnotify.ActionStyleDanger:    discordgo.DangerButton,
		hubnotify.ActionStyleLink:      discordgo.LinkButton,
	}
)

type actionCallbackRegistry struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]actionCallbackEntry
}

type actionCallbackEntry struct {
	callbackURL string
	expiresAt   time.Time
}

func newActionCallbackRegistry(ttl time.Duration) *actionCallbackRegistry {
	if ttl <= 0 {
		ttl = notifyActionCallbackTTL
	}
	return &actionCallbackRegistry{
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]actionCallbackEntry),
	}
}

func (r *actionCallbackRegistry) Register(callbackURL string) string {
	if r == nil {
		return ""
	}
	token := hubnotify.CallbackToken(callbackURL)
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneExpiredLocked(now)
	r.entries[token] = actionCallbackEntry{callbackURL: callbackURL, expiresAt: now.Add(r.ttl)}
	return token
}

func (r *actionCallbackRegistry) Resolve(token string) (string, bool) {
	if r == nil {
		return "", false
	}
	key := strings.TrimSpace(token)
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.entries[key]
	if !ok {
		return "", false
	}
	if !entry.expiresAt.After(now) {
		delete(r.entries, key)
		return "", false
	}
	return entry.callbackURL, true
}

func (r *actionCallbackRegistry) pruneExpiredLocked(now time.Time) {
	for token, entry := range r.entries {
		if !entry.expiresAt.After(now) {
			delete(r.entries, token)
		}
	}
}

func (h *hubHandler) notify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.isNotifyAuthorized(r) {
		h.log.Printf("unauthorized /notify request from %s", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload notifyPayload
	if err := httpjson.DecodeStrictJSONBody(r, &payload, 1<<20); err != nil {
		h.writeBadRequest(w, err.Error())
		return
	}

	if err := validateNotifyPayload(&payload); err != nil {
		h.writeBadRequest(w, err.Error())
		return
	}

	channelID, ok := h.channelNameToID[payload.TargetChannel]
	if !ok || channelID == "" {
		h.writeBadRequest(w, "unknown targetChannel; configure a channel mapping")
		return
	}

	dispatchCtx, cancel := context.WithTimeout(r.Context(), notifyDispatchTimeout)
	defer cancel()

	message, err := buildNotifyMessage(payload, h.actionCallbacks)
	if err != nil {
		h.writeBadRequest(w, err.Error())
		return
	}

	if _, err := h.session.ChannelMessageSendComplex(channelID, message, discordgo.WithContext(dispatchCtx)); err != nil {
		h.log.Printf("failed to send discord message (channel=%s severity=%s): %v", payload.TargetChannel, payload.Severity, err)
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusGatewayTimeout
		}
		http.Error(w, "failed to dispatch discord message", status)
		return
	}

	httpjson.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
}

func (h *hubHandler) isNotifyAuthorized(r *http.Request) bool {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}

	providedToken := strings.TrimSpace(parts[1])
	if providedToken == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(providedToken), []byte(h.notifyAuthToken)) == 1
}

func (h *hubHandler) writeBadRequest(w http.ResponseWriter, message string) {
	h.log.Printf("bad request: %s", message)
	http.Error(w, message, http.StatusBadRequest)
}

func validateNotifyPayload(payload *notifyPayload) error {
	payload.TargetChannel = strings.TrimSpace(payload.TargetChannel)
	payload.Service = strings.TrimSpace(payload.Service)
	payload.Event = strings.TrimSpace(payload.Event)
	payload.Severity = strings.ToLower(strings.TrimSpace(payload.Severity))
	payload.Title = strings.TrimSpace(payload.Title)
	payload.Summary = strings.TrimSpace(payload.Summary)
	payload.URL = strings.TrimSpace(payload.URL)
	payload.CallbackURL = strings.TrimSpace(payload.CallbackURL)
	payload.Visibility = strings.ToLower(strings.TrimSpace(payload.Visibility))

	for idx := range payload.Fields {
		payload.Fields[idx].Key = strings.TrimSpace(payload.Fields[idx].Key)
		payload.Fields[idx].Value = strings.TrimSpace(payload.Fields[idx].Value)
		payload.Fields[idx].Group = strings.TrimSpace(payload.Fields[idx].Group)
	}

	for idx := range payload.AllowedMentions.Parse {
		payload.AllowedMentions.Parse[idx] = strings.ToLower(strings.TrimSpace(payload.AllowedMentions.Parse[idx]))
	}
	for idx := range payload.Actions {
		payload.Actions[idx].ID = strings.TrimSpace(payload.Actions[idx].ID)
		payload.Actions[idx].Label = strings.TrimSpace(payload.Actions[idx].Label)
		payload.Actions[idx].Style = strings.ToLower(strings.TrimSpace(payload.Actions[idx].Style))
		payload.Actions[idx].URL = strings.TrimSpace(payload.Actions[idx].URL)
	}

	if payload.Version != hubnotify.Version2 {
		return errors.New("version must be 2")
	}
	if payload.TargetChannel == "" {
		return errors.New("targetChannel is required")
	}
	if payload.Service == "" {
		return errors.New("service is required")
	}
	if payload.Event == "" {
		return errors.New("event is required")
	}
	if _, ok := allowedSeverities[payload.Severity]; !ok {
		return errors.New("severity must be one of: info, warning, critical")
	}
	if payload.Title == "" {
		return errors.New("title is required")
	}
	if !notifyTitleRegex.MatchString(payload.Title) {
		return errors.New("title must match [SERVICE] EVENT format")
	}
	if payload.Title != formatNotifyTitle(payload.Service, payload.Event) {
		return errors.New("title must match the canonical [SERVICE] EVENT format for service/event")
	}
	if payload.Summary == "" {
		return errors.New("summary is required")
	}
	if len(payload.Summary) > maxNotifySummaryLen {
		return fmt.Errorf("summary must be %d chars or fewer", maxNotifySummaryLen)
	}
	if payload.Visibility == "" {
		return errors.New("visibility is required")
	}
	if payload.Visibility != hubnotify.VisibilityPublic {
		return errors.New("visibility must be public")
	}
	if payload.OccurredAt.IsZero() {
		return errors.New("occurredAt is required")
	}
	if len(payload.Fields) == 0 {
		return errors.New("fields must include at least one field")
	}
	if len(payload.Fields) > maxNotifyFieldCount {
		return fmt.Errorf("fields must include no more than %d fields", maxNotifyFieldCount)
	}
	if len(payload.Actions) > maxNotifyActionCount {
		return fmt.Errorf("actions must include no more than %d actions", maxNotifyActionCount)
	}
	if err := validateNotifyFields(payload.Fields); err != nil {
		return err
	}
	if err := validateNotifyActions(*payload); err != nil {
		return err
	}
	if err := validateAllowedMentions(payload.AllowedMentions); err != nil {
		return err
	}

	return nil
}

func buildNotifyMessage(payload notifyPayload, callbacks *actionCallbackRegistry) (*discordgo.MessageSend, error) {
	allowedMentions, err := toDiscordAllowedMentions(payload.AllowedMentions)
	if err != nil {
		return nil, err
	}

	message := &discordgo.MessageSend{
		Embeds:          []*discordgo.MessageEmbed{buildSeverityEmbed(payload)},
		AllowedMentions: allowedMentions,
	}
	if len(payload.Actions) > 0 {
		components, err := buildNotifyComponents(payload, callbacks)
		if err != nil {
			return nil, err
		}
		message.Components = components
	}
	if payload.SuppressNotifications {
		message.Flags = discordgo.MessageFlagsSuppressNotifications
	}

	return message, nil
}

func buildSeverityEmbed(payload notifyPayload) *discordgo.MessageEmbed {
	fields := make([]*discordgo.MessageEmbedField, 0, len(payload.Fields))
	for _, field := range payload.Fields {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   field.Key,
			Value:  field.Value,
			Inline: field.Inline,
		})
	}

	return &discordgo.MessageEmbed{
		Title:       payload.Title,
		Description: payload.Summary,
		URL:         payload.URL,
		Color:       notifySeverityColors[payload.Severity],
		Fields:      fields,
		Timestamp:   payload.OccurredAt.UTC().Format(time.RFC3339),
		Footer: &discordgo.MessageEmbedFooter{
			Text: encodeNotifyFooter(payload.Service, payload.Event),
		},
	}
}

func validateNotifyFields(fields []hubnotify.NotifyField) error {
	seenOrders := make(map[int]struct{}, len(fields))
	lastGroupRank := 0
	lastOrder := 0
	for _, field := range fields {
		if field.Key == "" {
			return errors.New("fields.key is required")
		}
		if len(field.Key) > maxNotifyFieldKeyLen {
			return fmt.Errorf("field %q key must be %d chars or fewer", field.Key, maxNotifyFieldKeyLen)
		}
		if field.Value == "" {
			return fmt.Errorf("field %q value is required", field.Key)
		}
		if len(field.Value) > maxNotifyFieldValLen {
			return fmt.Errorf("field %q value must be %d chars or fewer", field.Key, maxNotifyFieldValLen)
		}
		groupRank, ok := notifyGroupRank[field.Group]
		if !ok {
			return fmt.Errorf("field %q group must be one of: Context, Metrics, Timing, Links", field.Key)
		}
		if field.Order <= 0 {
			return fmt.Errorf("field %q order must be greater than zero", field.Key)
		}
		if _, ok := seenOrders[field.Order]; ok {
			return fmt.Errorf("field %q order %d is duplicated", field.Key, field.Order)
		}
		seenOrders[field.Order] = struct{}{}
		if groupRank < lastGroupRank || (groupRank == lastGroupRank && field.Order <= lastOrder) {
			return errors.New("fields must be ordered by group and then order")
		}
		lastGroupRank = groupRank
		lastOrder = field.Order
	}

	return nil
}

func validateAllowedMentions(mentions hubnotify.AllowedMentions) error {
	seen := make(map[string]struct{}, len(mentions.Parse))
	for _, value := range mentions.Parse {
		if _, ok := allowedMentionTypes[value]; !ok {
			return errors.New("allowedMentions.parse must only include users, roles, or everyone")
		}
		if _, ok := seen[value]; ok {
			return errors.New("allowedMentions.parse must not contain duplicates")
		}
		seen[value] = struct{}{}
	}
	if len(mentions.Users) > 0 {
		if _, ok := seen["users"]; ok {
			return errors.New("allowedMentions.users cannot be used when parse includes users")
		}
	}
	if len(mentions.Roles) > 0 {
		if _, ok := seen["roles"]; ok {
			return errors.New("allowedMentions.roles cannot be used when parse includes roles")
		}
	}
	for _, userID := range mentions.Users {
		_ = userID
	}
	for _, roleID := range mentions.Roles {
		_ = roleID
	}
	if err := validateMentionIDs("allowedMentions.users", mentions.Users); err != nil {
		return err
	}
	if err := validateMentionIDs("allowedMentions.roles", mentions.Roles); err != nil {
		return err
	}

	return nil
}

func validateMentionIDs(fieldName string, ids []string) error {
	if len(ids) > maxNotifyMentionCount {
		return fmt.Errorf("%s must contain no more than %d values", fieldName, maxNotifyMentionCount)
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		value := strings.TrimSpace(id)
		if value == "" {
			return fmt.Errorf("%s must not contain empty values", fieldName)
		}
		if !notifySnowflakeRegex.MatchString(value) {
			return fmt.Errorf("%s must only include Discord snowflake IDs", fieldName)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%s must not contain duplicates", fieldName)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateNotifyActions(payload notifyPayload) error {
	if len(payload.Actions) == 0 {
		if payload.CallbackURL != "" {
			return validateCallbackURL(payload.CallbackURL)
		}
		return nil
	}

	needsCallback := false
	seenIDs := make(map[string]struct{}, len(payload.Actions))
	for _, action := range payload.Actions {
		if action.Label == "" {
			return errors.New("action label is required")
		}
		if _, ok := allowedActionStyles[action.Style]; !ok {
			return fmt.Errorf("action %q style is invalid", action.Label)
		}
		if action.Style == hubnotify.ActionStyleLink {
			if action.ID != "" {
				return fmt.Errorf("action %q must not include id when style is link", action.Label)
			}
			if action.URL == "" {
				return fmt.Errorf("action %q url is required", action.Label)
			}
			if _, err := url.ParseRequestURI(action.URL); err != nil {
				return fmt.Errorf("action %q url must be a valid URI", action.Label)
			}
			continue
		}
		needsCallback = true
		if action.ID == "" {
			return fmt.Errorf("action %q id is required", action.Label)
		}
		if action.URL != "" {
			return fmt.Errorf("action %q must not include url unless style is link", action.Label)
		}
		if _, exists := seenIDs[action.ID]; exists {
			return fmt.Errorf("action id %q is duplicated", action.ID)
		}
		seenIDs[action.ID] = struct{}{}
		if len(encodeNotifyActionCustomID(hubnotify.CallbackToken(payload.CallbackURL), action.ID)) > componentIDMaxLen {
			return fmt.Errorf("action %q exceeds discord custom_id limit", action.Label)
		}
	}

	if needsCallback {
		return validateCallbackURL(payload.CallbackURL)
	}

	return nil
}

func validateCallbackURL(raw string) error {
	if raw == "" {
		return errors.New("callbackUrl is required when actions include non-link buttons")
	}
	if strings.Contains(raw, "|") {
		return errors.New("callbackUrl must not contain |")
	}
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return errors.New("callbackUrl must be a valid URI")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("callbackUrl must use http or https")
	}

	return nil
}

func buildNotifyComponents(payload notifyPayload, callbacks *actionCallbackRegistry) ([]discordgo.MessageComponent, error) {
	buttons := make([]discordgo.MessageComponent, 0, len(payload.Actions))
	callbackToken := ""
	if payload.CallbackURL != "" {
		callbackToken = callbacks.Register(payload.CallbackURL)
	}
	for _, action := range payload.Actions {
		button := discordgo.Button{
			Label: action.Label,
			Style: allowedActionStyles[action.Style],
		}
		if action.Style == hubnotify.ActionStyleLink {
			button.URL = action.URL
		} else {
			if callbackToken == "" {
				return nil, errors.New("callbackUrl could not be registered")
			}
			button.CustomID = encodeNotifyActionCustomID(callbackToken, action.ID)
		}
		buttons = append(buttons, button)
	}

	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: buttons}}, nil
}

func toDiscordAllowedMentions(mentions hubnotify.AllowedMentions) (*discordgo.MessageAllowedMentions, error) {
	parse := make([]discordgo.AllowedMentionType, 0, len(mentions.Parse))
	for _, value := range mentions.Parse {
		mentionType, ok := allowedMentionTypes[value]
		if !ok {
			return nil, errors.New("allowedMentions.parse includes unsupported mention type")
		}
		parse = append(parse, mentionType)
	}
	sort.Slice(parse, func(i, j int) bool {
		return string(parse[i]) < string(parse[j])
	})

	return &discordgo.MessageAllowedMentions{
		Parse:       parse,
		Users:       append([]string(nil), mentions.Users...),
		Roles:       append([]string(nil), mentions.Roles...),
		RepliedUser: mentions.RepliedUser,
	}, nil
}

func formatNotifyTitle(service string, event string) string {
	return hubnotify.CanonicalTitle(service, event)
}

func encodeNotifyFooter(service string, event string) string {
	return strings.TrimSpace(service) + notifyFooterSeparator + strings.TrimSpace(event)
}

func decodeNotifyFooter(raw string) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), notifyFooterSeparator, 2)
	if len(parts) != 2 {
		return "", "", errors.New("notify footer metadata is invalid")
	}
	service := strings.TrimSpace(parts[0])
	event := strings.TrimSpace(parts[1])
	if service == "" || event == "" {
		return "", "", errors.New("notify footer metadata is incomplete")
	}
	return service, event, nil
}

func encodeNotifyActionCustomID(callbackToken string, actionID string) string {
	return strings.Join([]string{componentIDPrefix, strings.TrimSpace(callbackToken), strings.TrimSpace(actionID)}, "|")
}

func decodeNotifyActionCustomID(customID string) (string, string, error) {
	parts := strings.Split(customID, "|")
	if len(parts) != 3 || parts[0] != componentIDPrefix {
		return "", "", errors.New("unsupported component custom_id")
	}
	if parts[1] == "" || parts[2] == "" {
		return "", "", errors.New("component custom_id is incomplete")
	}
	return parts[1], parts[2], nil
}
