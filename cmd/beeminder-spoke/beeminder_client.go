package main

import (
	"net/http"

	"personal-infrastructure/pkg/beeminder"
)

func newBeeminderClient(cfg config, httpClient *http.Client) *beeminder.Client {
	return beeminder.NewClient(
		beeminder.WithBaseURL(cfg.BeeminderBaseURL),
		beeminder.WithAuthToken(cfg.BeeminderAuthToken),
		beeminder.WithUsername(cfg.BeeminderUsername),
		beeminder.WithTimeout(cfg.HTTPTimeout),
		beeminder.WithDatapointsPerPage(cfg.DatapointsPerPage),
		beeminder.WithMaxDatapointPages(cfg.MaxDatapointPages),
		beeminder.WithHTTPClient(httpClient),
	)
}
