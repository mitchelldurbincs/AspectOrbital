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

type openAIVisionClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

func newOpenAIVisionClient(baseURL, apiKey, model string, httpClient *http.Client) *openAIVisionClient {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" || httpClient == nil {
		return nil
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "gpt-4.1-mini"
	}
	return &openAIVisionClient{baseURL: baseURL, apiKey: apiKey, model: model, httpClient: httpClient}
}

func (c *openAIVisionClient) EvaluateImage(ctx context.Context, imageURL, prompt string) (visionEvaluation, error) {
	if c == nil {
		return visionEvaluation{}, fmt.Errorf("openai client is not configured")
	}
	imageURL = strings.TrimSpace(imageURL)
	prompt = strings.TrimSpace(prompt)
	if imageURL == "" {
		return visionEvaluation{}, fmt.Errorf("image URL is required")
	}
	if prompt == "" {
		return visionEvaluation{}, fmt.Errorf("prompt is required")
	}

	bodyMap := map[string]any{
		"model":       c.model,
		"temperature": 0,
		"response_format": map[string]any{
			"type": "json_object",
		},
		"messages": []map[string]any{
			{
				"role":    "system",
				"content": "You are a strict image classifier. Return only JSON with keys: match (boolean), confidence (number 0..1), reason (string).",
			},
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": fmt.Sprintf("Does this image satisfy the following requirement? Requirement: %s", prompt)},
					{"type": "image_url", "image_url": map[string]any{"url": imageURL}},
				},
			},
		},
	}

	bodyBytes, err := json.Marshal(bodyMap)
	if err != nil {
		return visionEvaluation{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return visionEvaluation{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return visionEvaluation{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return visionEvaluation{}, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return visionEvaluation{}, fmt.Errorf("openai API error (%s): %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return visionEvaluation{}, err
	}
	if len(completion.Choices) == 0 {
		return visionEvaluation{}, fmt.Errorf("openai response contained no choices")
	}
	raw := strings.TrimSpace(completion.Choices[0].Message.Content)
	if raw == "" {
		return visionEvaluation{}, fmt.Errorf("openai response content was empty")
	}

	var parsed struct {
		Match      bool    `json:"match"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return visionEvaluation{}, fmt.Errorf("failed to parse openai classifier JSON: %w", err)
	}
	if parsed.Confidence < 0 {
		parsed.Confidence = 0
	}
	if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}

	return visionEvaluation{Match: parsed.Match, Confidence: parsed.Confidence, Reason: strings.TrimSpace(parsed.Reason)}, nil
}
