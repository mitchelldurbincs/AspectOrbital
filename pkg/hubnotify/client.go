package hubnotify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	notifyURL  string
	authToken  string
	httpClient *http.Client
}

type NotifyRequest struct {
	TargetChannel string `json:"targetChannel"`
	Message       string `json:"message"`
	Severity      string `json:"severity"`
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
