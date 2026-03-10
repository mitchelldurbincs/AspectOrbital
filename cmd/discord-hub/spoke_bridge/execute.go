package spokebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"personal-infrastructure/pkg/spokecontract"
)

func (b *Bridge) ExecuteCommand(ctx context.Context, commandName string, commandContext CommandContext, options map[string]any) (string, error) {
	if b == nil {
		return "", errors.New("spoke command bridge is disabled")
	}

	serviceName, ok := b.commandOwners[commandName]
	if !ok {
		return "", fmt.Errorf("unknown command %q", commandName)
	}
	service, ok := b.services[serviceName]
	if !ok {
		return "", fmt.Errorf("owning service %q not configured for command %q", serviceName, commandName)
	}

	request := commandRequest{Command: commandName, Context: commandContext}
	var err error
	if len(options) > 0 {
		request.Options, err = PruneCommandOptions(options)
		if err != nil {
			return "", fmt.Errorf("invalid spoke command request: %w", err)
		}
	}
	if err := spokecontract.ValidateCommandRequestSchema(spokecontract.CommandRequest(request)); err != nil {
		return "", fmt.Errorf("invalid spoke command request: %w", err)
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, service.ExecuteURL, bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if b.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+b.authToken)
	}

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
	if err != nil {
		return "", err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message := string(bytes.TrimSpace(body))
		if message == "" {
			message = resp.Status
		}
		return "", errors.New(message)
	}

	var response commandResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("invalid spoke command response: %w", err)
	}
	if err := spokecontract.ValidateCommandResponseSchema(spokecontract.CommandResponse(response)); err != nil {
		return "", fmt.Errorf("invalid spoke command response: %w", err)
	}

	return TruncateForDiscord(response.Message), nil
}

func FormatCommandFailure(err error) string {
	if err == nil {
		return ""
	}

	return TruncateForDiscord(commandFailurePrefix + err.Error())
}
