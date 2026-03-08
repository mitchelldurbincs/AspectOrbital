package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type plaidClient struct {
	baseURL      string
	clientID     string
	secret       string
	accessTokens []string
	clientName   string
	countryCodes []string
	language     string
	webhookURL   string
	httpClient   *http.Client
}

func newPlaidClient(cfg config, httpClient *http.Client) *plaidClient {
	return &plaidClient{
		baseURL:      cfg.PlaidBaseURL,
		clientID:     cfg.PlaidClientID,
		secret:       cfg.PlaidSecret,
		accessTokens: cfg.PlaidAccessTokens,
		clientName:   cfg.PlaidClientName,
		countryCodes: cfg.PlaidCountryCodes,
		language:     cfg.PlaidLanguage,
		webhookURL:   cfg.PlaidWebhookURL,
		httpClient:   httpClient,
	}
}

func (c *plaidClient) HasCredentials() bool {
	return strings.TrimSpace(c.clientID) != "" && strings.TrimSpace(c.secret) != ""
}

func (c *plaidClient) CreateLinkToken(ctx context.Context, clientUserID string) (plaidLinkToken, error) {
	if !c.HasCredentials() {
		return plaidLinkToken{}, errors.New("Plaid credentials are not configured")
	}

	request := map[string]any{
		"client_id":     c.clientID,
		"secret":        c.secret,
		"client_name":   firstNonEmpty(c.clientName, "Aspect Orbital Finance"),
		"country_codes": c.countryCodes,
		"language":      firstNonEmpty(c.language, "en"),
		"products":      []string{"transactions"},
		"user": map[string]string{
			"client_user_id": firstNonEmpty(clientUserID, "local-finance-user"),
		},
	}

	if webhook := strings.TrimSpace(c.webhookURL); webhook != "" {
		request["webhook"] = webhook
	}

	var response plaidLinkToken
	if err := c.post(ctx, "/link/token/create", request, &response); err != nil {
		return plaidLinkToken{}, err
	}

	return response, nil
}

func (c *plaidClient) ExchangePublicToken(ctx context.Context, publicToken string) (plaidPublicTokenExchange, error) {
	if !c.HasCredentials() {
		return plaidPublicTokenExchange{}, errors.New("Plaid credentials are not configured")
	}

	request := map[string]any{
		"client_id":    c.clientID,
		"secret":       c.secret,
		"public_token": strings.TrimSpace(publicToken),
	}

	var response plaidPublicTokenExchange
	if err := c.post(ctx, "/item/public_token/exchange", request, &response); err != nil {
		return plaidPublicTokenExchange{}, err
	}

	return response, nil
}

func (c *plaidClient) WeeklySubscriptions(ctx context.Context, start, end time.Time, location *time.Location) ([]subscriptionCharge, error) {
	if len(c.accessTokens) == 0 {
		return nil, errors.New("no Plaid access tokens configured")
	}

	unique := make(map[string]subscriptionCharge)

	for _, token := range c.accessTokens {
		accounts, institutionName, err := c.getAccounts(ctx, token)
		if err != nil {
			return nil, err
		}

		streams, err := c.getRecurringOutflowStreams(ctx, token)
		if err != nil {
			return nil, err
		}

		for _, stream := range streams {
			if strings.TrimSpace(stream.LastDate) == "" {
				continue
			}

			occurredAt, err := time.ParseInLocation("2006-01-02", stream.LastDate, location)
			if err != nil {
				continue
			}

			if occurredAt.Before(start) || !occurredAt.Before(end) {
				continue
			}

			merchant := firstNonEmpty(stream.MerchantName, stream.Description, "Unknown subscription")
			accountLabel := firstNonEmpty(accounts[stream.AccountID], "Unknown account")

			key := strings.TrimSpace(stream.StreamID)
			if key == "" {
				key = strings.ToLower(strings.TrimSpace(merchant)) + "|" + strings.TrimSpace(stream.AccountID)
			}

			candidate := subscriptionCharge{
				UniqueKey:    key,
				Merchant:     merchant,
				Amount:       stream.LastAmount,
				OccurredAt:   occurredAt,
				AccountLabel: accountLabel,
				Institution:  institutionName,
				StreamID:     stream.StreamID,
			}

			existing, found := unique[key]
			if !found || candidate.OccurredAt.After(existing.OccurredAt) {
				unique[key] = candidate
			}
		}
	}

	charges := make([]subscriptionCharge, 0, len(unique))
	for _, charge := range unique {
		charges = append(charges, charge)
	}

	sort.Slice(charges, func(i, j int) bool {
		left := charges[i]
		right := charges[j]

		if left.OccurredAt.Equal(right.OccurredAt) {
			return strings.ToLower(left.Merchant) < strings.ToLower(right.Merchant)
		}

		return left.OccurredAt.After(right.OccurredAt)
	})

	return charges, nil
}

func (c *plaidClient) getRecurringOutflowStreams(ctx context.Context, accessToken string) ([]plaidRecurringStream, error) {
	reqBody := map[string]any{
		"client_id":    c.clientID,
		"secret":       c.secret,
		"access_token": accessToken,
	}

	var resp plaidRecurringGetResponse
	if err := c.post(ctx, "/transactions/recurring/get", reqBody, &resp); err != nil {
		return nil, err
	}

	return resp.OutflowStreams, nil
}

func (c *plaidClient) getAccounts(ctx context.Context, accessToken string) (map[string]string, string, error) {
	reqBody := map[string]any{
		"client_id":    c.clientID,
		"secret":       c.secret,
		"access_token": accessToken,
	}

	var resp plaidAccountsGetResponse
	if err := c.post(ctx, "/accounts/get", reqBody, &resp); err != nil {
		return nil, "", err
	}

	labels := make(map[string]string, len(resp.Accounts))
	for _, account := range resp.Accounts {
		label := strings.TrimSpace(account.Name)
		if label == "" {
			label = strings.TrimSpace(account.OfficialName)
		}
		if label == "" {
			label = firstNonEmpty(account.Subtype, account.Type, "account")
		}

		if mask := strings.TrimSpace(account.Mask); mask != "" {
			label = fmt.Sprintf("%s ••••%s", label, mask)
		}

		if resp.Item.InstitutionName != "" {
			label = fmt.Sprintf("%s %s", resp.Item.InstitutionName, label)
		}

		labels[account.AccountID] = label
	}

	return labels, strings.TrimSpace(resp.Item.InstitutionName), nil
}

func (c *plaidClient) post(ctx context.Context, path string, requestPayload any, responsePayload any) error {
	body, err := json.Marshal(requestPayload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))

		var apiErr plaidAPIError
		if err := json.Unmarshal(responseBody, &apiErr); err == nil && apiErr.ErrorCode != "" {
			return fmt.Errorf("plaid %s failed: %s (%s) %s", path, apiErr.ErrorCode, apiErr.ErrorType, strings.TrimSpace(apiErr.ErrorMessage))
		}

		return fmt.Errorf("plaid %s failed (%s): %s", path, resp.Status, strings.TrimSpace(string(responseBody)))
	}

	if err := json.NewDecoder(resp.Body).Decode(responsePayload); err != nil {
		return fmt.Errorf("plaid %s decode error: %w", path, err)
	}

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}

	return ""
}

type plaidRecurringGetResponse struct {
	OutflowStreams []plaidRecurringStream `json:"outflow_streams"`
}

type plaidRecurringStream struct {
	StreamID     string  `json:"stream_id"`
	AccountID    string  `json:"account_id"`
	MerchantName string  `json:"merchant_name"`
	Description  string  `json:"description"`
	LastAmount   float64 `json:"last_amount"`
	LastDate     string  `json:"last_date"`
}

type plaidAccountsGetResponse struct {
	Accounts []plaidAccount `json:"accounts"`
	Item     plaidItem      `json:"item"`
}

type plaidAccount struct {
	AccountID    string `json:"account_id"`
	Name         string `json:"name"`
	OfficialName string `json:"official_name"`
	Mask         string `json:"mask"`
	Type         string `json:"type"`
	Subtype      string `json:"subtype"`
}

type plaidItem struct {
	InstitutionName string `json:"institution_name"`
}

type plaidAPIError struct {
	ErrorType    string `json:"error_type"`
	ErrorCode    string `json:"error_code"`
	ErrorMessage string `json:"error_message"`
}

type plaidLinkToken struct {
	LinkToken  string `json:"link_token"`
	Expiration string `json:"expiration"`
	RequestID  string `json:"request_id"`
}

type plaidPublicTokenExchange struct {
	AccessToken string `json:"access_token"`
	ItemID      string `json:"item_id"`
	RequestID   string `json:"request_id"`
}
