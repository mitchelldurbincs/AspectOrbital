package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

type openAIVisionClient struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

type chatCompletionRequest struct {
	Model          string                  `json:"model"`
	Temperature    float64                 `json:"temperature"`
	ResponseFormat chatCompletionJSONMode  `json:"response_format"`
	Messages       []chatCompletionMessage `json:"messages"`
}

type chatCompletionJSONMode struct {
	Type string `json:"type"`
}

type chatCompletionMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type chatCompletionContentPart struct {
	Type     string                  `json:"type"`
	Text     string                  `json:"text,omitempty"`
	ImageURL *chatCompletionImageURL `json:"image_url,omitempty"`
}

type chatCompletionImageURL struct {
	URL string `json:"url"`
}

type chatCompletionResponse struct {
	Choices []chatCompletionChoice `json:"choices"`
}

type chatCompletionChoice struct {
	Message chatCompletionMessageResponse `json:"message"`
}

type chatCompletionMessageResponse struct {
	Content string `json:"content"`
}

type openAIClassifierResponse struct {
	Match      bool    `json:"match"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
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
	imageURL, prompt, err := validateEvaluateImageInput(imageURL, prompt)
	if err != nil {
		return visionEvaluation{}, err
	}

	bodyBytes, err := c.buildChatCompletionRequestBody(imageURL, prompt)
	if err != nil {
		return visionEvaluation{}, err
	}

	req, err := c.newChatCompletionRequest(ctx, bodyBytes)
	if err != nil {
		return visionEvaluation{}, err
	}

	respBody, err := c.executeRequest(req)
	if err != nil {
		return visionEvaluation{}, err
	}

	raw, err := parseCompletionContent(respBody)
	if err != nil {
		return visionEvaluation{}, err
	}

	return parseVisionEvaluation(raw)
}

func validateEvaluateImageInput(imageURL, prompt string) (string, string, error) {
	var err error
	imageURL, err = validatePublicImageURL(imageURL)
	if err != nil {
		return "", "", err
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", "", fmt.Errorf("prompt is required")
	}
	return imageURL, prompt, nil
}

func validatePublicImageURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("image URL is required")
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed == nil {
		return "", fmt.Errorf("image URL is invalid")
	}
	if !parsed.IsAbs() || !strings.EqualFold(parsed.Scheme, "https") {
		return "", fmt.Errorf("image URL must be an https URL")
	}
	if strings.TrimSpace(parsed.Host) == "" || strings.TrimSpace(parsed.Hostname()) == "" {
		return "", fmt.Errorf("image URL host is required")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("image URL must not include credentials")
	}

	host := strings.TrimSpace(strings.ToLower(parsed.Hostname()))
	if host == "localhost" {
		return "", fmt.Errorf("image URL host must be public")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return "", fmt.Errorf("image URL host must be public")
		}
	}

	return parsed.String(), nil
}

func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}

	addr, err := netip.ParseAddr(ip.String())
	if err == nil {
		if addr.Is6() {
			if addr.Is6() && addr.Is4In6() {
				addr = addr.Unmap()
			}
			if addr.Is6() && addr.IsLinkLocalUnicast() {
				return false
			}
		}
		if carrierGradeNATPrefix.Contains(addr) {
			return false
		}
	}

	return true
}

var carrierGradeNATPrefix = netip.MustParsePrefix("100.64.0.0/10")

func (c *openAIVisionClient) buildChatCompletionRequestBody(imageURL, prompt string) ([]byte, error) {
	body := chatCompletionRequest{
		Model:       c.model,
		Temperature: 0,
		ResponseFormat: chatCompletionJSONMode{
			Type: "json_object",
		},
		Messages: []chatCompletionMessage{
			{
				Role:    "system",
				Content: "You are a strict image classifier. Return only JSON with keys: match (boolean), confidence (number 0..1), reason (string).",
			},
			{
				Role: "user",
				Content: []chatCompletionContentPart{
					{Type: "text", Text: fmt.Sprintf("Does this image satisfy the following requirement? Requirement: %s", prompt)},
					{Type: "image_url", ImageURL: &chatCompletionImageURL{URL: imageURL}},
				},
			},
		},
	}

	return json.Marshal(body)
}

func (c *openAIVisionClient) newChatCompletionRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	return req, nil
}

func (c *openAIVisionClient) executeRequest(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("openai API error (%s): %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	return respBody, nil
}

func parseCompletionContent(respBody []byte) (string, error) {
	var completion chatCompletionResponse
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return "", err
	}
	if len(completion.Choices) == 0 {
		return "", fmt.Errorf("openai response contained no choices")
	}

	raw := strings.TrimSpace(completion.Choices[0].Message.Content)
	if raw == "" {
		return "", fmt.Errorf("openai response content was empty")
	}

	return raw, nil
}

func parseVisionEvaluation(raw string) (visionEvaluation, error) {
	var parsed openAIClassifierResponse
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
