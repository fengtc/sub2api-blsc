package service

import (
	"encoding/json"
	"strings"
)

type copilotAnthropicRequestSummary struct {
	ParseError      string                         `json:"parse_error,omitempty"`
	Model           string                         `json:"model,omitempty"`
	Stream          bool                           `json:"stream"`
	MaxTokens       int                            `json:"max_tokens,omitempty"`
	MessageCount    int                            `json:"message_count"`
	RoleCounts      map[string]int                 `json:"role_counts,omitempty"`
	BlockTypeCounts map[string]int                 `json:"block_type_counts,omitempty"`
	ToolsCount      int                            `json:"tools_count,omitempty"`
	ToolChoiceType  string                         `json:"tool_choice_type,omitempty"`
	SystemKind      string                         `json:"system_kind,omitempty"`
	MessageShapes   []copilotAnthropicMessageShape `json:"message_shapes,omitempty"`
}

type copilotAnthropicMessageShape struct {
	Index          int            `json:"index"`
	Role           string         `json:"role,omitempty"`
	ContentKind    string         `json:"content_kind,omitempty"`
	BlockTypes     map[string]int `json:"block_types,omitempty"`
	TextBlocks     int            `json:"text_blocks,omitempty"`
	ToolUses       int            `json:"tool_uses,omitempty"`
	ToolResults    int            `json:"tool_results,omitempty"`
	ThinkingBlocks int            `json:"thinking_blocks,omitempty"`
	ImageBlocks    int            `json:"image_blocks,omitempty"`
}

type copilotOpenAIRequestSummary struct {
	ParseError        string                      `json:"parse_error,omitempty"`
	Model             string                      `json:"model,omitempty"`
	Stream            bool                        `json:"stream"`
	MessageCount      int                         `json:"message_count"`
	RoleCounts        map[string]int              `json:"role_counts,omitempty"`
	ContentKindCounts map[string]int              `json:"content_kind_counts,omitempty"`
	ToolsCount        int                         `json:"tools_count,omitempty"`
	ToolChoiceKind    string                      `json:"tool_choice_kind,omitempty"`
	HasMaxTokens      bool                        `json:"has_max_tokens,omitempty"`
	MessageShapes     []copilotOpenAIMessageShape `json:"message_shapes,omitempty"`
}

type copilotOpenAIMessageShape struct {
	Index         int    `json:"index"`
	Role          string `json:"role,omitempty"`
	ContentKind   string `json:"content_kind,omitempty"`
	ToolCalls     int    `json:"tool_calls,omitempty"`
	HasToolCallID bool   `json:"has_tool_call_id,omitempty"`
}

func summarizeCopilotAnthropicRequest(body []byte) copilotAnthropicRequestSummary {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		MaxTokens  int               `json:"max_tokens"`
		System     json.RawMessage   `json:"system"`
		Stream     bool              `json:"stream"`
		Tools      []json.RawMessage `json:"tools"`
		ToolChoice *struct {
			Type string `json:"type"`
		} `json:"tool_choice"`
	}

	summary := copilotAnthropicRequestSummary{}
	if err := json.Unmarshal(body, &req); err != nil {
		summary.ParseError = err.Error()
		return summary
	}

	summary.Model = req.Model
	summary.Stream = req.Stream
	summary.MaxTokens = req.MaxTokens
	summary.MessageCount = len(req.Messages)
	summary.RoleCounts = make(map[string]int)
	summary.BlockTypeCounts = make(map[string]int)
	summary.ToolsCount = len(req.Tools)
	summary.SystemKind = rawJSONKind(req.System)
	if req.ToolChoice != nil {
		summary.ToolChoiceType = req.ToolChoice.Type
	}

	for i, msg := range req.Messages {
		role := strings.TrimSpace(msg.Role)
		summary.RoleCounts[role]++
		shape := copilotAnthropicMessageShape{
			Index:       i,
			Role:        role,
			ContentKind: rawJSONKind(msg.Content),
		}
		if shape.ContentKind == "array" {
			shape.BlockTypes = make(map[string]int)
			countAnthropicBlocks(msg.Content, shape.BlockTypes)
			for blockType, count := range shape.BlockTypes {
				summary.BlockTypeCounts[blockType] += count
			}
			shape.TextBlocks = shape.BlockTypes["text"]
			shape.ToolUses = shape.BlockTypes["tool_use"]
			shape.ToolResults = shape.BlockTypes["tool_result"]
			shape.ThinkingBlocks = shape.BlockTypes["thinking"]
			shape.ImageBlocks = shape.BlockTypes["image"]
		}
		summary.MessageShapes = append(summary.MessageShapes, shape)
	}

	return summary
}

func summarizeCopilotOpenAIRequest(body []byte) copilotOpenAIRequestSummary {
	var req struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role       string            `json:"role"`
			Content    json.RawMessage   `json:"content"`
			ToolCallID string            `json:"tool_call_id"`
			ToolCalls  []json.RawMessage `json:"tool_calls"`
		} `json:"messages"`
		Tools      []json.RawMessage `json:"tools"`
		ToolChoice json.RawMessage   `json:"tool_choice"`
		MaxTokens  *int              `json:"max_tokens"`
	}

	summary := copilotOpenAIRequestSummary{}
	if err := json.Unmarshal(body, &req); err != nil {
		summary.ParseError = err.Error()
		return summary
	}

	summary.Model = req.Model
	summary.Stream = req.Stream
	summary.MessageCount = len(req.Messages)
	summary.RoleCounts = make(map[string]int)
	summary.ContentKindCounts = make(map[string]int)
	summary.ToolsCount = len(req.Tools)
	summary.ToolChoiceKind = rawJSONKind(req.ToolChoice)
	summary.HasMaxTokens = req.MaxTokens != nil

	for i, msg := range req.Messages {
		role := strings.TrimSpace(msg.Role)
		contentKind := rawJSONKind(msg.Content)
		summary.RoleCounts[role]++
		summary.ContentKindCounts[contentKind]++
		summary.MessageShapes = append(summary.MessageShapes, copilotOpenAIMessageShape{
			Index:         i,
			Role:          role,
			ContentKind:   contentKind,
			ToolCalls:     len(msg.ToolCalls),
			HasToolCallID: msg.ToolCallID != "",
		})
	}

	return summary
}

func countAnthropicBlocks(raw json.RawMessage, counts map[string]int) {
	var blocks []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		counts["unparseable"]++
		return
	}
	for _, block := range blocks {
		blockType := strings.TrimSpace(block.Type)
		if blockType == "" {
			blockType = "unknown"
		}
		counts[blockType]++
	}
}

func rawJSONKind(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "missing"
	}
	switch trimmed[0] {
	case 'n':
		if trimmed == "null" {
			return "null"
		}
	case '"':
		return "string"
	case '[':
		return "array"
	case '{':
		return "object"
	case 't', 'f':
		return "bool"
	default:
		if (trimmed[0] >= '0' && trimmed[0] <= '9') || trimmed[0] == '-' {
			return "number"
		}
	}
	return "unknown"
}
