package beeminder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	authToken  string
	username   string
	httpClient *http.Client
}

type DatapointRequest struct {
	GoalSlug string
	Value    float64
	Comment  string
	Time     time.Time
}

func NewClient(baseURL, authToken, username string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://www.beeminder.com/api/v1"
	}

	return &Client{baseURL: base, authToken: strings.TrimSpace(authToken), username: strings.TrimSpace(username), httpClient: httpClient}
}

func (c *Client) CreateDatapoint(ctx context.Context, req DatapointRequest) error {
	if strings.TrimSpace(req.GoalSlug) == "" {
		return fmt.Errorf("goal slug is required")
	}
	if c == nil || c.authToken == "" || c.username == "" {
		return fmt.Errorf("beeminder client is not configured")
	}

	payload := map[string]any{
		"auth_token": c.authToken,
		"value":      req.Value,
		"comment":    strings.TrimSpace(req.Comment),
	}
	if !req.Time.IsZero() {
		payload["timestamp"] = req.Time.UTC().Unix()
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("%s/users/%s/goals/%s/datapoints.json", c.baseURL, url.PathEscape(c.username), url.PathEscape(strings.TrimSpace(req.GoalSlug)))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
	return fmt.Errorf("beeminder datapoint failed (%s): %s", resp.Status, strings.TrimSpace(string(respBody)))
}
