package spokebridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func (b *Bridge) ExecuteCommand(ctx context.Context, commandName string, options map[string]any) (string, error) {
	if b == nil {
		return "", errors.New("spoke command bridge is disabled")
	}

	request := commandRequest{Command: commandName}
	if len(options) > 0 {
		request.Options = pruneCommandOptions(options)
	}

	if argument, ok := request.Options[legacyArgumentOption].(string); ok {
		request.Argument = strings.TrimSpace(argument)
	}

	requestBody, err := json.Marshal(request)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.commandURL, bytes.NewReader(requestBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

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
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return "", errors.New(message)
	}

	var response commandResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("invalid spoke command response: %w", err)
	}

	message := strings.TrimSpace(response.Message)
	if message == "" {
		message = fmt.Sprintf("Command `%s` acknowledged.", commandName)
	}

	return truncateForDiscord(message), nil
}

func FormatCommandFailure(err error) string {
	if err == nil {
		return ""
	}

	return truncateForDiscord(commandFailurePrefix + err.Error())
}
