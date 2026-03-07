package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type beeminderClient struct {
	baseURL           string
	authToken         string
	username          string
	goalSlug          string
	datapointsPerPage int
	maxDatapointPages int
	httpClient        *http.Client
}

type beeminderUser struct {
	Username string `json:"username"`
	Timezone string `json:"timezone"`
}

type beeminderGoal struct {
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Rate     *float64 `json:"rate"`
	Runits   string   `json:"runits"`
	Deadline int      `json:"deadline"`
	GUnits   string   `json:"gunits"`
	AggDay   string   `json:"aggday"`
}

type beeminderDatapoint struct {
	ID        string  `json:"id"`
	Timestamp int64   `json:"timestamp"`
	Daystamp  string  `json:"daystamp"`
	Value     float64 `json:"value"`
	IsDummy   bool    `json:"is_dummy"`
}

func newBeeminderClient(cfg config, httpClient *http.Client) *beeminderClient {
	return &beeminderClient{
		baseURL:           cfg.BeeminderBaseURL,
		authToken:         cfg.BeeminderAuthToken,
		username:          cfg.BeeminderUsername,
		goalSlug:          cfg.BeeminderGoalSlug,
		datapointsPerPage: cfg.DatapointsPerPage,
		maxDatapointPages: cfg.MaxDatapointPages,
		httpClient:        httpClient,
	}
}

func (c *beeminderClient) GetUser(ctx context.Context) (beeminderUser, error) {
	var user beeminderUser

	path := "/users/" + url.PathEscape(c.username) + ".json"
	if err := c.getJSON(ctx, path, nil, &user); err != nil {
		return beeminderUser{}, err
	}

	if strings.TrimSpace(user.Timezone) == "" {
		return beeminderUser{}, fmt.Errorf("beeminder user %q returned no timezone", c.username)
	}

	return user, nil
}

func (c *beeminderClient) GetGoal(ctx context.Context) (beeminderGoal, error) {
	var goal beeminderGoal

	path := "/users/" + url.PathEscape(c.username) + "/goals/" + url.PathEscape(c.goalSlug) + ".json"
	if err := c.getJSON(ctx, path, nil, &goal); err != nil {
		return beeminderGoal{}, err
	}

	if goal.Slug == "" {
		goal.Slug = c.goalSlug
	}

	return goal, nil
}

func (c *beeminderClient) GetDatapointsForDay(ctx context.Context, daystamp string) ([]beeminderDatapoint, error) {
	datapoints := make([]beeminderDatapoint, 0, c.datapointsPerPage)

	path := "/users/" + url.PathEscape(c.username) + "/goals/" + url.PathEscape(c.goalSlug) + "/datapoints.json"

	for page := 1; page <= c.maxDatapointPages; page++ {
		query := url.Values{}
		query.Set("sort", "timestamp")
		query.Set("per", strconv.Itoa(c.datapointsPerPage))
		query.Set("page", strconv.Itoa(page))

		var pageData []beeminderDatapoint
		if err := c.getJSON(ctx, path, query, &pageData); err != nil {
			return nil, err
		}

		if len(pageData) == 0 {
			return datapoints, nil
		}

		stop := false
		for _, datapoint := range pageData {
			switch {
			case datapoint.Daystamp == daystamp:
				datapoints = append(datapoints, datapoint)
			case datapoint.Daystamp < daystamp:
				stop = true
			}
		}

		if stop {
			return datapoints, nil
		}

		if len(pageData) < c.datapointsPerPage {
			return datapoints, nil
		}
	}

	return nil, fmt.Errorf("datapoint pagination exceeded %d pages while reading daystamp %s", c.maxDatapointPages, daystamp)
}

func (c *beeminderClient) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	if query == nil {
		query = make(url.Values)
	}
	query.Set("auth_token", c.authToken)

	fullURL := c.baseURL + path + "?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return fmt.Errorf("beeminder API request failed (%s): %s", resp.Status, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("beeminder API decode error: %w", err)
	}

	return nil
}
