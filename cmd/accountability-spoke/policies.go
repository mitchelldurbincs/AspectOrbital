package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"personal-infrastructure/pkg/accountability"
	"personal-infrastructure/pkg/spokecontract"
)

const (
	policyFileVersion        = 1
	policyEngineAttachment   = "manual_attachment"
	policyEngineTextReply    = "text_reply"
	policyEngineOpenAIVision = "openai_vision"
)

type visionEvaluator interface {
	EvaluateImage(context.Context, string, string) (visionEvaluation, error)
}

type visionEvaluation struct {
	Match      bool
	Confidence float64
	Reason     string
}

type policyFile struct {
	Version       int                     `json:"version"`
	DefaultPreset string                  `json:"defaultPreset"`
	Presets       map[string]policyPreset `json:"presets"`
}

type policyPreset struct {
	Task         string         `json:"task"`
	Engine       string         `json:"engine"`
	EngineConfig map[string]any `json:"engineConfig"`
}

type policyCatalog struct {
	defaultPreset string
	presets       map[string]resolvedPreset
	vision        visionEvaluator
}

type resolvedPreset struct {
	Name       string
	Task       string
	Engine     string
	ConfigJSON string
}

type resolvedCommitPolicy struct {
	Task       string
	Preset     string
	Engine     string
	ConfigJSON string
}

type proofEvaluation struct {
	Pass    bool
	Reason  string
	Verdict string
}

func loadPolicyCatalog(path string, vision visionEvaluator) (policyCatalog, error) {
	rawPath := strings.TrimSpace(path)
	if rawPath == "" {
		return policyCatalog{}, errors.New("ACCOUNTABILITY_POLICY_FILE is required")
	}
	body, err := os.ReadFile(rawPath)
	if err != nil {
		return policyCatalog{}, fmt.Errorf("failed to read policy file %q: %w", rawPath, err)
	}

	var cfg policyFile
	if err := json.Unmarshal(body, &cfg); err != nil {
		return policyCatalog{}, fmt.Errorf("invalid policy file JSON: %w", err)
	}
	if cfg.Version != policyFileVersion {
		return policyCatalog{}, fmt.Errorf("policy file version must be %d", policyFileVersion)
	}
	if len(cfg.Presets) == 0 {
		return policyCatalog{}, errors.New("policy file must include at least one preset")
	}

	catalog := policyCatalog{presets: make(map[string]resolvedPreset, len(cfg.Presets)), vision: vision}
	for rawName, preset := range cfg.Presets {
		name := strings.ToLower(strings.TrimSpace(rawName))
		if err := spokecontract.ValidateCommandName(name); err != nil {
			return policyCatalog{}, fmt.Errorf("invalid preset name %q: %w", rawName, err)
		}
		if _, exists := catalog.presets[name]; exists {
			return policyCatalog{}, fmt.Errorf("duplicate preset name %q", name)
		}

		task := strings.TrimSpace(preset.Task)
		if task == "" {
			return policyCatalog{}, fmt.Errorf("preset %q task is required", name)
		}

		engine := normalizePolicyEngine(preset.Engine)
		if engine == "" {
			return policyCatalog{}, fmt.Errorf("preset %q engine is required", name)
		}

		_, configJSON, err := normalizePolicyConfig(engine, preset.EngineConfig)
		if err != nil {
			return policyCatalog{}, fmt.Errorf("preset %q config error: %w", name, err)
		}
		if engine == policyEngineOpenAIVision && vision == nil {
			return policyCatalog{}, fmt.Errorf("preset %q uses openai_vision but OpenAI is not configured", name)
		}

		catalog.presets[name] = resolvedPreset{
			Name:       name,
			Task:       task,
			Engine:     engine,
			ConfigJSON: configJSON,
		}
	}

	catalog.defaultPreset = strings.ToLower(strings.TrimSpace(cfg.DefaultPreset))
	if catalog.defaultPreset == "" {
		return policyCatalog{}, errors.New("defaultPreset is required")
	}
	if _, ok := catalog.presets[catalog.defaultPreset]; !ok {
		return policyCatalog{}, fmt.Errorf("defaultPreset %q does not exist in presets", catalog.defaultPreset)
	}

	return catalog, nil
}

func (c policyCatalog) ResolveCommit(taskInput, presetInput string) (resolvedCommitPolicy, error) {
	presetName := strings.ToLower(strings.TrimSpace(presetInput))
	if presetName == "" {
		presetName = c.defaultPreset
	}
	preset, ok := c.presets[presetName]
	if !ok {
		return resolvedCommitPolicy{}, fmt.Errorf("unknown preset %q", presetName)
	}

	task := strings.TrimSpace(taskInput)
	if task == "" {
		task = preset.Task
	}
	if task == "" {
		return resolvedCommitPolicy{}, errors.New("task is required and preset does not define a default task")
	}

	return resolvedCommitPolicy{Task: task, Preset: preset.Name, Engine: preset.Engine, ConfigJSON: preset.ConfigJSON}, nil
}

func (c policyCatalog) Evaluate(ctx context.Context, commitment accountability.Commitment, attachment accountability.AttachmentMetadata, proofText string) (proofEvaluation, error) {
	engine := normalizePolicyEngine(commitment.PolicyEngine)
	if engine == "" {
		engine = policyEngineAttachment
	}
	configRaw := strings.TrimSpace(commitment.PolicyConfig)
	if configRaw == "" {
		configRaw = "{}"
	}
	config := map[string]any{}
	if err := json.Unmarshal([]byte(configRaw), &config); err != nil {
		return proofEvaluation{}, fmt.Errorf("invalid stored policy config: %w", err)
	}
	normalized, _, err := normalizePolicyConfig(engine, config)
	if err != nil {
		return proofEvaluation{}, err
	}

	proofText = strings.TrimSpace(proofText)
	switch engine {
	case policyEngineAttachment:
		if strings.TrimSpace(attachment.ID) == "" {
			return proofEvaluation{Pass: false, Reason: "this commitment requires an attachment proof"}, nil
		}
		return proofEvaluation{Pass: true, Verdict: "manual_attachment:accepted"}, nil
	case policyEngineTextReply:
		minChars, err := intFromAny(normalized["minChars"])
		if err != nil {
			return proofEvaluation{}, err
		}
		if len(proofText) < minChars {
			return proofEvaluation{Pass: false, Reason: fmt.Sprintf("this commitment requires a text reply of at least %d characters", minChars)}, nil
		}
		return proofEvaluation{Pass: true, Verdict: fmt.Sprintf("text_reply:accepted(minChars=%d)", minChars)}, nil
	case policyEngineOpenAIVision:
		if c.vision == nil {
			return proofEvaluation{}, errors.New("openai_vision is not configured")
		}
		attachmentURL := strings.TrimSpace(attachment.URL)
		if attachmentURL == "" {
			return proofEvaluation{Pass: false, Reason: "this commitment requires an attachment with a URL"}, nil
		}
		validatedURL, err := validatePublicImageURL(attachmentURL)
		if err != nil {
			return proofEvaluation{Pass: false, Reason: fmt.Sprintf("invalid proof attachment URL: %v", err)}, nil
		}
		contentType := strings.ToLower(strings.TrimSpace(attachment.ContentType))
		if contentType != "" && !strings.HasPrefix(contentType, "image/") {
			return proofEvaluation{Pass: false, Reason: fmt.Sprintf("proof attachment content type must be image/* (got %q)", attachment.ContentType)}, nil
		}
		prompt := strings.TrimSpace(fmt.Sprint(normalized["prompt"]))
		minConfidence, err := floatFromAny(normalized["minConfidence"])
		if err != nil {
			return proofEvaluation{}, err
		}
		result, err := c.vision.EvaluateImage(ctx, validatedURL, prompt)
		if err != nil {
			return proofEvaluation{}, fmt.Errorf("openai vision check failed: %w", err)
		}
		if !result.Match || result.Confidence < minConfidence {
			reason := strings.TrimSpace(result.Reason)
			if reason == "" {
				reason = "image did not satisfy policy requirement"
			}
			return proofEvaluation{Pass: false, Reason: fmt.Sprintf("vision check did not pass (confidence %.2f < %.2f): %s", result.Confidence, minConfidence, reason)}, nil
		}
		return proofEvaluation{Pass: true, Verdict: fmt.Sprintf("openai_vision:accepted(confidence=%.2f)", result.Confidence)}, nil
	default:
		return proofEvaluation{}, fmt.Errorf("unsupported policy engine %q", engine)
	}
}

func normalizePolicyEngine(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizePolicyConfig(engine string, raw map[string]any) (map[string]any, string, error) {
	if raw == nil {
		raw = map[string]any{}
	}

	normalized := map[string]any{}
	switch engine {
	case policyEngineAttachment:
		// no required config
	case policyEngineTextReply:
		minChars := 1
		if v, ok := raw["minChars"]; ok {
			n, err := intFromAny(v)
			if err != nil {
				return nil, "", fmt.Errorf("minChars must be an integer: %w", err)
			}
			minChars = n
		}
		if minChars <= 0 {
			return nil, "", errors.New("minChars must be positive")
		}
		normalized["minChars"] = minChars
	case policyEngineOpenAIVision:
		prompt := strings.TrimSpace(fmt.Sprint(raw["prompt"]))
		if prompt == "" {
			return nil, "", errors.New("prompt is required for openai_vision")
		}
		minConfidence := 0.75
		if v, ok := raw["minConfidence"]; ok {
			f, err := floatFromAny(v)
			if err != nil {
				return nil, "", fmt.Errorf("minConfidence must be numeric: %w", err)
			}
			minConfidence = f
		}
		if minConfidence <= 0 || minConfidence > 1 {
			return nil, "", errors.New("minConfidence must be > 0 and <= 1")
		}
		normalized["prompt"] = prompt
		normalized["minConfidence"] = minConfidence
	default:
		return nil, "", fmt.Errorf("unsupported policy engine %q", engine)
	}

	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, "", fmt.Errorf("failed to encode normalized config: %w", err)
	}
	return normalized, string(encoded), nil
}

func intFromAny(v any) (int, error) {
	switch value := v.(type) {
	case int:
		return value, nil
	case int32:
		return int(value), nil
	case int64:
		return int(value), nil
	case float64:
		if float64(int(value)) != value {
			return 0, errors.New("must be a whole number")
		}
		return int(value), nil
	case json.Number:
		i, err := value.Int64()
		if err != nil {
			return 0, err
		}
		return int(i), nil
	default:
		return 0, fmt.Errorf("unsupported type %T", v)
	}
}

func floatFromAny(v any) (float64, error) {
	switch value := v.(type) {
	case float64:
		return value, nil
	case float32:
		return float64(value), nil
	case int:
		return float64(value), nil
	case int32:
		return float64(value), nil
	case int64:
		return float64(value), nil
	case json.Number:
		f, err := value.Float64()
		if err != nil {
			return 0, err
		}
		return f, nil
	default:
		return 0, fmt.Errorf("unsupported type %T", v)
	}
}
