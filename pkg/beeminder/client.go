package beeminder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL           = "https://www.beeminder.com/api/v1"
	defaultTimeout           = 10 * time.Second
	defaultDatapointsPerPage = 100
	defaultMaxDatapointPages = 20
)

type Client struct {
	baseURL           string
	authToken         string
	username          string
	datapointsPerPage int
	maxDatapointPages int
	httpClient        *http.Client
}

// DatapointRequest is the request shape consumed by existing callers.
type DatapointRequest struct {
	GoalSlug string
	Value    float64
	Comment  string
	Time     time.Time
}

// CreateDatapointRequest is the lower-level request shape used by the richer API.
type CreateDatapointRequest struct {
	Value     float64
	Comment   string
	Timestamp *int64
	RequestID string
}

type User struct {
	Username string `json:"username"`
	Timezone string `json:"timezone"`
}

type Goal struct {
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Rate     *float64 `json:"rate"`
	Runits   string   `json:"runits"`
	Deadline int      `json:"deadline"`
	GUnits   string   `json:"gunits"`
	AggDay   string   `json:"aggday"`
}

type Datapoint struct {
	ID        string  `json:"id"`
	Timestamp int64   `json:"timestamp"`
	Daystamp  string  `json:"daystamp"`
	Value     float64 `json:"value"`
	IsDummy   bool    `json:"is_dummy"`
}

func NewClient(baseURL, authToken, username string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = defaultBaseURL
	}

	return &Client{
		baseURL:           base,
		authToken:         strings.TrimSpace(authToken),
		username:          strings.TrimSpace(username),
		datapointsPerPage: defaultDatapointsPerPage,
		maxDatapointPages: defaultMaxDatapointPages,
		httpClient:        httpClient,
	}
}

func (c *Client) GetUser(ctx context.Context) (User, error) {
	var user User

	path := "/users/" + url.PathEscape(c.username) + ".json"
	if err := c.getJSON(ctx, path, nil, &user); err != nil {
		return User{}, err
	}

	if strings.TrimSpace(user.Timezone) == "" {
		return User{}, fmt.Errorf("beeminder user %q returned no timezone", c.username)
	}

	return user, nil
}

func (c *Client) GetGoal(ctx context.Context, goalSlug string) (Goal, error) {
	var goal Goal

	path := "/users/" + url.PathEscape(c.username) + "/goals/" + url.PathEscape(goalSlug) + ".json"
	if err := c.getJSON(ctx, path, nil, &goal); err != nil {
		return Goal{}, err
	}

	if goal.Slug == "" {
		goal.Slug = goalSlug
	}

	return goal, nil
}

func (c *Client) GetDatapointsForDay(ctx context.Context, goalSlug, daystamp string) ([]Datapoint, error) {
	datapoints := make([]Datapoint, 0, c.datapointsPerPage)

	path := "/users/" + url.PathEscape(c.username) + "/goals/" + url.PathEscape(goalSlug) + "/datapoints.json"
	orderKnown := false
	descendingByTimestamp := true
	hasPrevPageLast := false
	var prevPageLastTimestamp int64

	for page := 1; page <= c.maxDatapointPages; page++ {
		query := url.Values{}
		query.Set("sort", "timestamp")
		query.Set("per", strconv.Itoa(c.datapointsPerPage))
		query.Set("page", strconv.Itoa(page))

		var pageData []Datapoint
		if err := c.getJSON(ctx, path, query, &pageData); err != nil {
			return nil, err
		}

		if len(pageData) == 0 {
			return datapoints, nil
		}

		if !orderKnown {
			switch {
			case len(pageData) > 1:
				descendingByTimestamp = pageData[0].Timestamp >= pageData[len(pageData)-1].Timestamp
				orderKnown = true
			case hasPrevPageLast:
				descendingByTimestamp = prevPageLastTimestamp >= pageData[0].Timestamp
				orderKnown = true
			}
		}

		prevPageLastTimestamp = pageData[len(pageData)-1].Timestamp
		hasPrevPageLast = true

		stop := false
		for _, datapoint := range pageData {
			switch {
			case datapoint.Daystamp == daystamp:
				datapoints = append(datapoints, datapoint)
			case orderKnown && descendingByTimestamp && datapoint.Daystamp < daystamp:
				stop = true
			case orderKnown && !descendingByTimestamp && datapoint.Daystamp > daystamp:
				stop = true
			}

			if stop {
				break
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

// CreateDatapoint keeps the existing package API used by the accountability service.
func (c *Client) CreateDatapoint(ctx context.Context, req DatapointRequest) error {
	if strings.TrimSpace(req.GoalSlug) == "" {
		return fmt.Errorf("goal slug is required")
	}
	if c == nil || c.authToken == "" || c.username == "" {
		return fmt.Errorf("beeminder client is not configured")
	}

	createReq := CreateDatapointRequest{Value: req.Value, Comment: strings.TrimSpace(req.Comment)}
	if !req.Time.IsZero() {
		ts := req.Time.UTC().Unix()
		createReq.Timestamp = &ts
	}

	_, err := c.CreateGoalDatapoint(ctx, req.GoalSlug, createReq)
	return err
}

// CreateGoalDatapoint provides the richer datapoint response-oriented API.
func (c *Client) CreateGoalDatapoint(ctx context.Context, goalSlug string, request CreateDatapointRequest) (Datapoint, error) {
	if strings.TrimSpace(goalSlug) == "" {
		return Datapoint{}, fmt.Errorf("goal slug is required")
	}
	if c == nil || c.authToken == "" || c.username == "" {
		return Datapoint{}, fmt.Errorf("beeminder client is not configured")
	}

	payload := map[string]any{"value": request.Value}
	if request.Comment != "" {
		payload["comment"] = request.Comment
	}
	if request.Timestamp != nil {
		payload["timestamp"] = *request.Timestamp
	}
	if request.RequestID != "" {
		payload["requestid"] = request.RequestID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Datapoint{}, fmt.Errorf("beeminder API encode error: %w", err)
	}

	path := "/users/me/goals/" + url.PathEscape(strings.TrimSpace(goalSlug)) + "/datapoints.json"
	fullURL := c.withAuthToken(path, nil)

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, bytes.NewReader(body))
	if err != nil {
		return Datapoint{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return Datapoint{}, err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 8*1024))
		return Datapoint{}, fmt.Errorf("beeminder API request failed (%s): %s", response.Status, strings.TrimSpace(string(responseBody)))
	}

	var datapoint Datapoint
	if err := json.NewDecoder(response.Body).Decode(&datapoint); err != nil {
		return Datapoint{}, fmt.Errorf("beeminder API decode error: %w", err)
	}

	return datapoint, nil
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	fullURL := c.withAuthToken(path, query)

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

func (c *Client) withAuthToken(path string, query url.Values) string {
	if query == nil {
		query = make(url.Values)
	}
	query.Set("auth_token", c.authToken)
	return c.baseURL + path + "?" + query.Encode()
}
