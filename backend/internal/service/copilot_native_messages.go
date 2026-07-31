package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/copilot"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	copilotNativeMessagesAPIVersion   = "2026-06-01"
	copilotNativeMessagesEditor       = "vscode/1.109.3"
	copilotNativeMessagesPlugin       = "copilot-chat/0.58.0"
	copilotNativeMessagesUserAgent    = "vscode_claude_code/2.1.112 (external, sdk-ts, agent-sdk/0.2.112)"
	copilotNativeMessagesVersion      = "2023-06-01"
	copilotNativeMessagesFallbackBody = 4 << 10
)

var copilotNativeMessagesAllowedBetas = map[string]struct{}{
	"advanced-tool-use-2025-11-20":    {},
	"context-management-2025-06-27":   {},
	"interleaved-thinking-2025-05-14": {},
}

func (s *CopilotGatewayService) tryForwardNativeMessages(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	startTime time.Time,
	isStream bool,
	summary copilotAnthropicRequestSummary,
) (*CopilotForwardResult, bool, error) {
	nativeBody, model, metadataUserID, err := prepareCopilotNativeMessagesBody(body, account.GetModelMapping())
	if err != nil {
		return nil, true, fmt.Errorf("copilot messages: prepare native request: %w", err)
	}
	if !shouldUseCopilotNativeMessages(model) {
		return nil, false, nil
	}

	token, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return nil, true, fmt.Errorf("copilot messages: native auth: %w", err)
	}
	baseURL, err := s.resolveBaseURL(account)
	if err != nil {
		return nil, true, fmt.Errorf("copilot messages: native base URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", bytes.NewReader(nativeBody))
	if err != nil {
		return nil, true, fmt.Errorf("copilot messages: build native request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	for key, values := range copilotNativeMessagesHeaders(c, nativeBody, metadataUserID) {
		for _, value := range values {
			req.Header.Set(key, value)
		}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("copilot messages: native upstream request: %w", err)
	}

	slog.Debug("copilot native messages upstream response",
		"account_id", account.ID,
		"model", model,
		"status", resp.StatusCode,
		"stream", isStream,
		"latency_ms", time.Since(startTime).Milliseconds())

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, copilotNativeMessagesFallbackBody))
		_ = resp.Body.Close()
		slog.Debug("copilot native messages unavailable; falling back to chat completions",
			"account_id", account.ID,
			"model", model,
			"status", resp.StatusCode)
		return nil, false, nil
	}

	if resp.StatusCode != http.StatusOK {
		result, handleErr := s.handleErrorResponse(c, resp, account, map[string]any{
			"endpoint":          "native_messages",
			"anthropic_request": summary,
		})
		return result, true, handleErr
	}

	if isStream {
		result, streamErr := s.handleNativeMessagesStreamingResponse(c, resp, model, startTime)
		return result, true, streamErr
	}
	result, responseErr := s.handleNativeMessagesNonStreamingResponse(c, resp, model)
	return result, true, responseErr
}

func prepareCopilotNativeMessagesBody(
	body []byte,
	modelMapping map[string]string,
) ([]byte, string, string, error) {
	if !json.Valid(body) {
		return nil, "", "", fmt.Errorf("invalid JSON")
	}

	originalModel := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if originalModel == "" {
		return nil, "", "", fmt.Errorf("model is required")
	}
	model := normalizeCopilotModel(originalModel, modelMapping)

	out := body
	if model != originalModel {
		var err error
		out, err = sjson.SetBytes(out, "model", model)
		if err != nil {
			return nil, "", "", fmt.Errorf("map model: %w", err)
		}
	}

	// Copilot's native Messages endpoint accepts Anthropic cache breakpoints,
	// but not the optional scope extension. Keep client-owned breakpoints,
	// add stable tool/message anchors when absent, and enforce Anthropic's
	// four-breakpoint limit.
	out = stripCopilotNativeCacheControlScope(out)
	out = applyToolsLastCacheBreakpoint(out)
	out = addMessageCacheBreakpoints(out)
	out = enforceCacheControlLimit(out)

	return out, model, gjson.GetBytes(out, "metadata.user_id").String(), nil
}

func stripCopilotNativeCacheControlScope(body []byte) []byte {
	invalidThinking, messagePaths, toolPaths, systemPaths := collectCacheControlPaths(body)
	paths := make([]string, 0, len(invalidThinking)+len(messagePaths)+len(toolPaths)+len(systemPaths))
	for _, item := range invalidThinking {
		paths = append(paths, item.path)
	}
	paths = append(paths, messagePaths...)
	paths = append(paths, toolPaths...)
	paths = append(paths, systemPaths...)

	out := body
	for _, path := range paths {
		scopePath := path + ".scope"
		if !gjson.GetBytes(out, scopePath).Exists() {
			continue
		}
		next, err := sjson.DeleteBytes(out, scopePath)
		if err == nil {
			out = next
		}
	}
	return out
}

func shouldUseCopilotNativeMessages(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "claude-")
}

func copilotNativeMessagesHeaders(c *gin.Context, body []byte, metadataUserID string) http.Header {
	headers := copilot.CopilotHeaders(copilotNativeMessagesInitiator(body), copilotNativeMessagesHasVision(body))
	requestID := headers.Get("x-request-id")

	headers.Set("Accept", "application/json")
	headers.Set("Content-Type", "application/json")
	headers.Set("editor-version", copilotNativeMessagesEditor)
	headers.Set("editor-plugin-version", copilotNativeMessagesPlugin)
	headers.Set("User-Agent", copilotNativeMessagesUserAgent)
	headers.Set("x-github-api-version", copilotNativeMessagesAPIVersion)
	headers.Set("x-agent-task-id", requestID)
	headers.Set("x-interaction-type", "messages-proxy")
	headers.Set("openai-intent", "messages-proxy")
	headers.Del("copilot-integration-id")

	if parsed := ParseMetadataUserID(metadataUserID); parsed != nil {
		headers.Set("editor-device-id", parsed.DeviceID)
		headers.Set("x-interaction-id", parsed.SessionID)
	}

	anthropicVersion := copilotNativeMessagesVersion
	if c != nil && c.Request != nil {
		if incoming := strings.TrimSpace(c.GetHeader("anthropic-version")); incoming != "" {
			anthropicVersion = incoming
		}
		if beta := filterCopilotNativeMessagesBetas(c.GetHeader("anthropic-beta")); beta != "" {
			headers.Set("anthropic-beta", beta)
		}
	}
	headers.Set("anthropic-version", anthropicVersion)

	return headers
}

func filterCopilotNativeMessagesBetas(raw string) string {
	var allowed []string
	seen := make(map[string]struct{})
	for _, beta := range parseAnthropicBetaHeader(raw) {
		if _, ok := copilotNativeMessagesAllowedBetas[beta]; !ok {
			continue
		}
		if _, duplicate := seen[beta]; duplicate {
			continue
		}
		seen[beta] = struct{}{}
		allowed = append(allowed, beta)
	}
	return strings.Join(allowed, ",")
}

func copilotNativeMessagesInitiator(body []byte) string {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return "user"
	}
	items := messages.Array()
	if len(items) == 0 {
		return "user"
	}
	last := items[len(items)-1]
	if last.Get("role").String() != "user" {
		return "agent"
	}
	content := last.Get("content")
	if content.Type == gjson.String {
		return "user"
	}
	if !content.IsArray() {
		return "user"
	}
	for _, block := range content.Array() {
		if block.Get("type").String() != "tool_result" {
			return "user"
		}
	}
	return "agent"
}

func copilotNativeMessagesHasVision(body []byte) bool {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return false
	}

	hasVision := false
	messages.ForEach(func(_, message gjson.Result) bool {
		content := message.Get("content")
		if !content.IsArray() {
			return true
		}
		content.ForEach(func(_, block gjson.Result) bool {
			if block.Get("type").String() == "image" {
				hasVision = true
				return false
			}
			if block.Get("type").String() != "tool_result" {
				return true
			}
			inner := block.Get("content")
			if !inner.IsArray() {
				return true
			}
			inner.ForEach(func(_, item gjson.Result) bool {
				if item.Get("type").String() == "image" {
					hasVision = true
					return false
				}
				return true
			})
			return !hasVision
		})
		return !hasVision
	})
	return hasVision
}

func (u *CopilotUsage) applyClaudeUsage(usage *ClaudeUsage) {
	if u == nil || usage == nil {
		return
	}
	inputTokens := max(usage.InputTokens, 0)
	u.CacheCreationInputTokens = max(usage.CacheCreationInputTokens, 0)
	u.CacheReadInputTokens = max(usage.CacheReadInputTokens, 0)
	u.PromptTokens = inputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
	u.CompletionTokens = max(usage.OutputTokens, 0)
	u.TotalTokens = u.PromptTokens + u.CompletionTokens
}

func (s *CopilotGatewayService) handleNativeMessagesNonStreamingResponse(
	c *gin.Context,
	resp *http.Response,
	model string,
) (*CopilotForwardResult, error) {
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot native messages: read response: %w", err)
	}

	usage := &CopilotUsage{}
	usage.applyClaudeUsage(parseClaudeUsageFromResponseBody(body))

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(http.StatusOK, contentType, body)
	return &CopilotForwardResult{
		StatusCode: http.StatusOK,
		Model:      model,
		Usage:      usage,
	}, nil
}

func (s *CopilotGatewayService) handleNativeMessagesStreamingResponse(
	c *gin.Context,
	resp *http.Response,
	model string,
	startTime time.Time,
) (*CopilotForwardResult, error) {
	defer func() { _ = resp.Body.Close() }()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("copilot native messages: response writer does not support flushing")
	}

	claudeUsage := &ClaudeUsage{}
	var firstTokenMs *int
	clientDisconnected := false
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if data, ok := extractAnthropicSSEDataLine(line); ok {
			data = strings.TrimSpace(data)
			parseAnthropicSSEUsage(data, claudeUsage)
			if firstTokenMs == nil && copilotNativeMessagesEventStartsOutput(data) {
				ms := int(time.Since(startTime).Milliseconds())
				firstTokenMs = &ms
			}
		}

		if !clientDisconnected {
			if _, err := fmt.Fprintln(c.Writer, line); err != nil {
				clientDisconnected = true
			} else if line == "" {
				flusher.Flush()
			}
		}
	}

	usage := &CopilotUsage{}
	usage.applyClaudeUsage(claudeUsage)
	if err := scanner.Err(); err != nil {
		return &CopilotForwardResult{
			StatusCode:   http.StatusOK,
			Model:        model,
			Usage:        usage,
			FirstTokenMs: firstTokenMs,
		}, fmt.Errorf("copilot native messages: stream usage incomplete: %w", err)
	}

	return &CopilotForwardResult{
		StatusCode:   http.StatusOK,
		Model:        model,
		Usage:        usage,
		FirstTokenMs: firstTokenMs,
	}, nil
}

func copilotNativeMessagesEventStartsOutput(data string) bool {
	if data == "" || data == "[DONE]" {
		return false
	}
	var event apicompat.AnthropicStreamEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return false
	}
	switch event.Type {
	case "content_block_start":
		return event.ContentBlock != nil
	case "content_block_delta":
		return event.Delta != nil &&
			(event.Delta.Text != "" || event.Delta.PartialJSON != "" || event.Delta.Thinking != "")
	default:
		return false
	}
}
