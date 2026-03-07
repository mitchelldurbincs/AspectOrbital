package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type hubClient struct {
	notifyURL  string
	httpClient *http.Client
}

type hubNotifyRequest struct {
	TargetChannel string `json:"targetChannel"`
	Message       string `json:"message"`
	Severity      string `json:"severity"`
}

func newHubClient(cfg config, httpClient *http.Client) *hubClient {
	return &hubClient{
		notifyURL:  cfg.HubNotifyURL,
		httpClient: httpClient,
	}
}

func (c *hubClient) Notify(ctx context.Context, payload hubNotifyRequest) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.notifyURL, bytes.NewReader(body))
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
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return fmt.Errorf("discord-hub notify failed (%s): %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	return nil
}
