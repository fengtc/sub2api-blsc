package service

import (
	"strings"

	"github.com/tidwall/gjson"
)

func claudeModelDisallowsTemperature(model string) bool {
	normalized := strings.ToLower(strings.TrimSpace(model))
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 {
		normalized = normalized[idx+1:]
	}
	normalized = strings.ReplaceAll(normalized, ".", "-")
	return strings.Contains(normalized, "claude-opus-4-7") ||
		strings.Contains(normalized, "claude-opus-4-8")
}

func sanitizeDeprecatedClaudeSamplingParams(body []byte, modelID string) ([]byte, bool) {
	if len(body) == 0 {
		return body, false
	}
	model := strings.TrimSpace(modelID)
	if model == "" {
		model = gjson.GetBytes(body, "model").String()
	}
	if !claudeModelDisallowsTemperature(model) || !gjson.GetBytes(body, "temperature").Exists() {
		return body, false
	}
	next, ok := deleteJSONPathBytes(body, "temperature")
	if !ok {
		return body, false
	}
	return next, true
}
