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
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/copilot"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
)

// claudeModelDotPattern matches Claude model IDs with dot-separated versions,
// e.g. "claude-sonnet-4.5", "claude-opus-4.6", "claude-haiku-4.5".
// Used to rewrite these for clients that expect dash-separated versions.
var claudeModelDotPattern = regexp.MustCompile(`claude-(?:sonnet|opus|haiku)-\d+\.\d+`)

const (
	copilotMonthlyQuotaExceededReason = "copilot_monthly_quota_exceeded"
	copilotAccountStateUpdateTimeout  = 5 * time.Second
)

type copilotTempUnschedulableRepository interface {
	SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error
}

// CopilotGatewayService handles forwarding requests to the GitHub Copilot API.
//
// It supports:
//   - /chat/completions (OpenAI-compatible format, streaming and non-streaming)
//   - /models (list available models)
//
// Authentication is handled via CopilotTokenProvider, which exchanges
// GitHub tokens for short-lived Copilot API tokens.
type CopilotGatewayService struct {
	tokenProvider         *CopilotTokenProvider
	tempUnschedulableRepo copilotTempUnschedulableRepository
	httpClient            *http.Client
	cfg                   *config.Config
}

// NewCopilotGatewayService creates a new CopilotGatewayService.
func NewCopilotGatewayService(
	tokenProvider *CopilotTokenProvider,
	configs ...*config.Config,
) *CopilotGatewayService {
	var cfg *config.Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
	var tempUnschedulableRepo copilotTempUnschedulableRepository
	if tokenProvider != nil {
		tempUnschedulableRepo = tokenProvider.accountRepo
	}
	return &CopilotGatewayService{
		tokenProvider:         tokenProvider,
		tempUnschedulableRepo: tempUnschedulableRepo,
		cfg:                   cfg,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // long timeout for streaming
		},
	}
}

func (s *CopilotGatewayService) resolveBaseURL(account *Account) (string, error) {
	customURL := strings.TrimSpace(account.GetCredential("base_url"))
	if customURL == "" {
		return copilot.CopilotAPIBase, nil
	}
	if s.cfg == nil {
		return strings.TrimRight(customURL, "/"), nil
	}
	if !s.cfg.Security.URLAllowlist.Enabled {
		normalized, err := urlvalidator.ValidateURLFormat(customURL, s.cfg.Security.URLAllowlist.AllowInsecureHTTP)
		if err != nil {
			return "", err
		}
		return strings.TrimRight(normalized, "/"), nil
	}
	normalized, err := urlvalidator.ValidateHTTPSURL(customURL, urlvalidator.ValidationOptions{
		AllowedHosts:     s.cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: true,
		AllowPrivate:     s.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
	if err != nil {
		return "", err
	}
	return strings.TrimRight(normalized, "/"), nil
}

// CopilotForwardResult holds the result of a Copilot API request.
type CopilotForwardResult struct {
	StatusCode       int
	Model            string
	Usage            *CopilotUsage
	FirstTokenMs     *int
	UpstreamEndpoint string
}

// CopilotUsage tracks token usage from a Copilot API response.
type CopilotUsage struct {
	PromptTokens             int `json:"prompt_tokens"`
	CompletionTokens         int `json:"completion_tokens"`
	TotalTokens              int `json:"total_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

func (u *CopilotUsage) applyChatUsage(usage *apicompat.ChatUsage) {
	if u == nil || usage == nil {
		return
	}
	u.PromptTokens = max(usage.PromptTokens, 0)
	u.CompletionTokens = max(usage.CompletionTokens, 0)
	u.TotalTokens = max(usage.TotalTokens, 0)
	u.CacheCreationInputTokens = 0
	u.CacheReadInputTokens = 0
	if usage.PromptTokensDetails == nil {
		return
	}
	u.CacheReadInputTokens = max(usage.PromptTokensDetails.CachedTokens, 0)
	if usage.PromptTokensDetails.CacheWriteTokens > 0 {
		u.CacheCreationInputTokens = usage.PromptTokensDetails.CacheWriteTokens
	} else {
		u.CacheCreationInputTokens = max(usage.PromptTokensDetails.CacheCreationTokens, 0)
	}
}

// UncachedPromptTokens converts OpenAI prompt token semantics (total prompt
// tokens) into Anthropic input token semantics (excluding cache read/write).
func (u *CopilotUsage) UncachedPromptTokens() int {
	if u == nil {
		return 0
	}
	return max(u.PromptTokens-u.CacheReadInputTokens-u.CacheCreationInputTokens, 0)
}

// ForwardChatCompletions forwards a chat/completions request to the Copilot API.
func (s *CopilotGatewayService) ForwardChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*CopilotForwardResult, error) {
	startTime := time.Now()

	// Get Copilot API token
	token, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("copilot auth: %w", err)
	}

	// Determine base URL
	baseURL, err := s.resolveBaseURL(account)
	if err != nil {
		return nil, fmt.Errorf("copilot base URL: %w", err)
	}

	// Apply model mapping if configured
	body, model := s.applyModelMapping(body, account)

	// Detect streaming mode
	isStream := detectStreamMode(body)

	// Build upstream request
	upstreamURL := baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("copilot: build request: %w", err)
	}

	// Set Copilot-specific headers
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	for k, vals := range copilot.CopilotHeaders("user", false) {
		for _, v := range vals {
			req.Header.Set(k, v)
		}
	}

	// Send request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot: upstream request: %w", err)
	}

	slog.Debug("copilot upstream response",
		"account_id", account.ID,
		"model", model,
		"status", resp.StatusCode,
		"stream", isStream,
		"latency_ms", time.Since(startTime).Milliseconds())

	// Handle error responses
	if resp.StatusCode != http.StatusOK {
		return s.handleErrorResponse(c, resp, account, map[string]any{
			"endpoint":       "chat_completions",
			"openai_request": summarizeCopilotOpenAIRequest(body),
		})
	}

	// Handle streaming response
	if isStream {
		return s.handleStreamingResponse(c, resp, model, startTime)
	}

	// Handle non-streaming response
	return s.handleNonStreamingResponse(c, resp, model)
}

// handleStreamingResponse proxies SSE streaming from Copilot API to the client.
func (s *CopilotGatewayService) handleStreamingResponse(
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
		return nil, fmt.Errorf("copilot: response writer does not support flushing")
	}

	usage := &CopilotUsage{}
	var firstTokenMs *int
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Parse usage from SSE data
		if strings.HasPrefix(line, "data: ") {
			data := line[6:]
			if data != "[DONE]" {
				s.parseStreamUsage(data, usage)
				var chunk apicompat.ChatCompletionsChunk
				if err := json.Unmarshal([]byte(data), &chunk); err == nil &&
					firstTokenMs == nil &&
					chatChunkStartsResponsesOutput(&chunk) {
					ms := int(time.Since(startTime).Milliseconds())
					firstTokenMs = &ms
				}
			}
		}

		// Forward line to client
		fmt.Fprintf(c.Writer, "%s\n", line)
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("copilot stream scanner error", "error", err)
	}

	return &CopilotForwardResult{
		StatusCode:       http.StatusOK,
		Model:            model,
		Usage:            usage,
		FirstTokenMs:     firstTokenMs,
		UpstreamEndpoint: "/chat/completions",
	}, nil
}

// handleNonStreamingResponse proxies a non-streaming response from Copilot API.
func (s *CopilotGatewayService) handleNonStreamingResponse(
	c *gin.Context,
	resp *http.Response,
	model string,
) (*CopilotForwardResult, error) {
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot: read response: %w", err)
	}

	// Extract usage
	usage := s.parseNonStreamUsage(body)

	// Forward response headers
	for k, vals := range resp.Header {
		for _, v := range vals {
			c.Header(k, v)
		}
	}
	c.Data(http.StatusOK, "application/json", body)

	return &CopilotForwardResult{
		StatusCode:       http.StatusOK,
		Model:            model,
		Usage:            usage,
		UpstreamEndpoint: "/chat/completions",
	}, nil
}

// handleErrorResponse handles non-200 responses from the Copilot API.
func (s *CopilotGatewayService) handleErrorResponse(
	c *gin.Context,
	resp *http.Response,
	account *Account,
	diagnostics map[string]any,
) (*CopilotForwardResult, error) {
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	logAttrs := []any{
		"account_id", account.ID,
		"status", resp.StatusCode,
		"body", string(body),
	}
	if len(diagnostics) > 0 {
		logAttrs = append(logAttrs, "diagnostics", diagnostics)
	}
	slog.Warn("copilot upstream error", logAttrs...)

	if isCopilotQuotaExceededResponse(resp.StatusCode, body) {
		until := nextCopilotMonthlyQuotaReset(time.Now())
		s.persistCopilotMonthlyQuotaExceeded(c, account, until)
		return nil, &UpstreamFailoverError{
			StatusCode:              resp.StatusCode,
			ResponseBody:            body,
			ResponseHeaders:         resp.Header.Clone(),
			TempUnschedulableUntil:  &until,
			TempUnschedulableReason: copilotMonthlyQuotaExceededReason,
		}
	}

	// Handle specific error codes
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		// Token may have expired, invalidate cache
		s.tokenProvider.InvalidateToken(account.ID)
	case http.StatusTooManyRequests:
		// Rate limited — caller should handle retry/failover
	}

	// Forward error to client
	c.Data(resp.StatusCode, "application/json", body)

	return &CopilotForwardResult{
		StatusCode: resp.StatusCode,
	}, nil
}

func isCopilotQuotaExceededResponse(statusCode int, body []byte) bool {
	if statusCode != http.StatusPaymentRequired {
		return false
	}
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	return payload.Error.Code == "quota_exceeded"
}

func nextCopilotMonthlyQuotaReset(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

func (s *CopilotGatewayService) persistCopilotMonthlyQuotaExceeded(c *gin.Context, account *Account, until time.Time) {
	accountID := int64(0)
	if account != nil {
		accountID = account.ID
	}
	if s == nil || s.tempUnschedulableRepo == nil || accountID <= 0 {
		slog.Error("copilot monthly quota temp unschedulable persistence unavailable",
			"account_id", accountID,
			"until", until,
		)
		return
	}

	baseCtx := context.Background()
	if c != nil && c.Request != nil {
		baseCtx = context.WithoutCancel(c.Request.Context())
	}
	stateCtx, cancel := context.WithTimeout(baseCtx, copilotAccountStateUpdateTimeout)
	defer cancel()

	if err := s.tempUnschedulableRepo.SetTempUnschedulable(stateCtx, accountID, until, copilotMonthlyQuotaExceededReason); err != nil {
		slog.Error("copilot monthly quota temp unschedulable persistence failed",
			"account_id", accountID,
			"until", until,
			"error", err,
		)
		return
	}
	slog.Warn("copilot monthly quota exhausted; account temporarily unschedulable",
		"account_id", accountID,
		"until", until,
	)
}

func extractRequestModel(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Model
}

// applyModelMapping applies model mapping from account configuration.
func (s *CopilotGatewayService) applyModelMapping(body []byte, account *Account) ([]byte, string) {
	originalModel := extractRequestModel(body)
	if originalModel == "" {
		return body, ""
	}

	mappedModel := account.GetMappedModel(originalModel)

	if mappedModel != originalModel {
		// Simple approach: replace model field in the JSON.
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err == nil {
			modelBytes, err := json.Marshal(mappedModel)
			if err == nil {
				raw["model"] = modelBytes
				if replaced, err := json.Marshal(raw); err == nil {
					slog.Debug("copilot model mapping", "original", originalModel, "mapped", mappedModel)
					return replaced, originalModel
				}
			}
		}
	}

	return body, originalModel
}

// detectStreamMode checks if the request body has "stream": true.
func detectStreamMode(body []byte) bool {
	var req struct {
		Stream any `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	switch v := req.Stream.(type) {
	case bool:
		return v
	default:
		return false
	}
}

// parseStreamUsage extracts usage data from an SSE data line.
func (s *CopilotGatewayService) parseStreamUsage(data string, usage *CopilotUsage) {
	var chunk struct {
		Usage *apicompat.ChatUsage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(data), &chunk); err == nil && chunk.Usage != nil {
		usage.applyChatUsage(chunk.Usage)
	}
}

// parseNonStreamUsage extracts usage data from a non-streaming response body.
func (s *CopilotGatewayService) parseNonStreamUsage(body []byte) *CopilotUsage {
	var resp struct {
		Usage *apicompat.ChatUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err == nil && resp.Usage != nil {
		usage := &CopilotUsage{}
		usage.applyChatUsage(resp.Usage)
		return usage
	}
	return &CopilotUsage{}
}

// ListModels returns the list of models available on the Copilot API.
func (s *CopilotGatewayService) ListModels(
	ctx context.Context,
	account *Account,
) ([]byte, error) {
	token, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("copilot auth: %w", err)
	}

	baseURL, err := s.resolveBaseURL(account)
	if err != nil {
		return nil, fmt.Errorf("copilot base URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("copilot: build models request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	for k, vals := range copilot.CopilotHeaders("user", false) {
		for _, v := range vals {
			req.Header.Set(k, v)
		}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot: models request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot: read models response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot: models HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Rewrite model IDs: replace dots with dashes in Claude model names so that
	// Claude Code's built-in model whitelist accepts them.
	// e.g. "claude-sonnet-4.5" → "claude-sonnet-4-5"
	// The reverse mapping is applied in normalizeCopilotModel when forwarding requests.
	body = rewriteModelIDsForClient(body)

	return body, nil
}

// rewriteModelIDsForClient rewrites Claude model IDs in a Copilot /models JSON
// response, replacing dots with dashes so that Claude Code's built-in model
// whitelist accepts them.
//
// e.g. "claude-sonnet-4.5" → "claude-sonnet-4-5"
//
// Only Claude model IDs are rewritten; GPT and other models are left unchanged.
func rewriteModelIDsForClient(body []byte) []byte {
	// Simple string replacement on the raw JSON — fast and avoids full parse/re-encode.
	// We replace patterns like "claude-xxx-N.M" → "claude-xxx-N-M".
	result := claudeModelDotPattern.ReplaceAllFunc(body, func(match []byte) []byte {
		return bytes.ReplaceAll(match, []byte{'.'}, []byte{'-'})
	})
	return result
}

// Anthropic /v1/messages gateway
// ─────────────────────────────────────────────────────────────────────────────

// ForwardMessages receives an Anthropic /v1/messages request, translates it to
// OpenAI /chat/completions format, forwards it to the Copilot API, and
// translates the response back to Anthropic format.
//
// This allows Claude Code (and any Anthropic-compatible client) to use GitHub
// Copilot accounts as the backend without any client-side changes.
func (s *CopilotGatewayService) ForwardMessages(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	anthropicBody []byte,
) (*CopilotForwardResult, error) {
	startTime := time.Now()

	// Detect streaming before translation (we need to know for the response path).
	isStream := detectAnthropicStream(anthropicBody)
	anthropicSummary := summarizeCopilotAnthropicRequest(anthropicBody)

	if result, handled, err := s.tryForwardNativeMessages(
		ctx,
		c,
		account,
		anthropicBody,
		startTime,
		isStream,
		anthropicSummary,
	); handled {
		return result, err
	}

	// Translate Anthropic request → OpenAI format.
	openAIBody, err := translateAnthropicToOpenAI(anthropicBody, account.GetModelMapping())
	if err != nil {
		return nil, fmt.Errorf("copilot messages: translate request: %w", err)
	}

	model := extractRequestModel(openAIBody)

	// Get Copilot API token.
	token, err := s.tokenProvider.GetAccessToken(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("copilot messages: auth: %w", err)
	}

	// Determine base URL.
	baseURL, err := s.resolveBaseURL(account)
	if err != nil {
		return nil, fmt.Errorf("copilot base URL: %w", err)
	}

	// Build upstream request to Copilot /chat/completions.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(openAIBody))
	if err != nil {
		return nil, fmt.Errorf("copilot messages: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	for k, vals := range copilot.CopilotHeaders("user", false) {
		for _, v := range vals {
			req.Header.Set(k, v)
		}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot messages: upstream request: %w", err)
	}

	slog.Debug("copilot messages upstream response",
		"account_id", account.ID,
		"model", model,
		"status", resp.StatusCode,
		"stream", isStream,
		"latency_ms", time.Since(startTime).Milliseconds())

	if resp.StatusCode != http.StatusOK {
		return s.handleErrorResponse(c, resp, account, map[string]any{
			"endpoint":          "messages",
			"anthropic_request": anthropicSummary,
			"openai_request":    summarizeCopilotOpenAIRequest(openAIBody),
		})
	}

	if isStream {
		return s.handleMessagesStreamingResponse(c, resp, model, startTime)
	}
	return s.handleMessagesNonStreamingResponse(c, resp, model)
}

// handleMessagesNonStreamingResponse reads the OpenAI response and writes back
// an Anthropic-format JSON response.
func (s *CopilotGatewayService) handleMessagesNonStreamingResponse(
	c *gin.Context,
	resp *http.Response,
	model string,
) (*CopilotForwardResult, error) {
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot messages: read response: %w", err)
	}

	usage := s.parseNonStreamUsage(body)

	// Translate OpenAI response → Anthropic format.
	anthropicBody, err := translateOpenAIToAnthropic(body)
	if err != nil {
		slog.Warn("copilot messages: failed to translate response",
			"error", err, "model", model)
		return &CopilotForwardResult{
			StatusCode:       http.StatusOK,
			Model:            model,
			Usage:            usage,
			UpstreamEndpoint: "/chat/completions",
		}, fmt.Errorf("copilot messages: invalid chat completions response: %w", err)
	}

	c.Data(http.StatusOK, "application/json", anthropicBody)
	return &CopilotForwardResult{
		StatusCode:       http.StatusOK,
		Model:            model,
		Usage:            usage,
		UpstreamEndpoint: "/chat/completions",
	}, nil
}

// handleMessagesStreamingResponse reads the Copilot SSE stream, translates each
// OpenAI chunk to Anthropic SSE events, and writes them to the client.
func (s *CopilotGatewayService) handleMessagesStreamingResponse(
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
		return nil, fmt.Errorf("copilot messages: response writer does not support flushing")
	}

	state := apicompat.NewChatCompletionsToAnthropicStreamState(model)
	usage := &CopilotUsage{}
	var firstTokenMs *int
	clientDisconnected := false
	sawDone := false

	emitEvents := func(events []apicompat.AnthropicStreamEvent) {
		if clientDisconnected {
			return
		}
		for _, evt := range events {
			sse, err := apicompat.ResponsesAnthropicEventToSSE(evt)
			if err != nil {
				slog.Debug("copilot messages stream: failed to marshal event",
					"event_type", evt.Type, "error", err)
				continue
			}
			if _, err := fmt.Fprint(c.Writer, sse); err != nil {
				clientDisconnected = true
				break
			}
		}
		if !clientDisconnected && len(events) > 0 {
			flusher.Flush()
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		data, ok := extractOpenAISSEDataLine(line)
		if !ok {
			// Preserve SSE comments as downstream keepalives during long
			// Copilot TTFT waits. Event-name and blank framing lines belong to
			// the upstream OpenAI stream and must not leak into Anthropic SSE.
			if strings.HasPrefix(line, ":") && !clientDisconnected {
				if _, err := fmt.Fprintf(c.Writer, "%s\n\n", line); err != nil {
					clientDisconnected = true
				} else {
					flusher.Flush()
				}
			}
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			sawDone = true
			break
		}

		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			slog.Debug("copilot messages stream: skip unparseable chunk", "data", data)
			continue
		}

		// Accumulate usage.
		if chunk.Usage != nil {
			usage.applyChatUsage(chunk.Usage)
		}

		if firstTokenMs == nil && chatChunkStartsResponsesOutput(&chunk) {
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}

		// Copilot commonly sends usage in a trailing choices:[] chunk. Feed
		// every chunk to the shared bridge and defer terminal events until
		// [DONE]/EOF so the final message_delta carries the exact usage.
		emitEvents(apicompat.ChatCompletionsChunkToAnthropicEvents(&chunk, state))
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("copilot messages stream scanner error", "error", err)
		return &CopilotForwardResult{
			StatusCode:       http.StatusOK,
			Model:            model,
			Usage:            usage,
			FirstTokenMs:     firstTokenMs,
			UpstreamEndpoint: "/chat/completions",
		}, fmt.Errorf("copilot messages: stream usage incomplete: %w", err)
	}

	emitEvents(apicompat.FinalizeChatCompletionsAnthropicStream(state))
	if !sawDone {
		slog.Debug("copilot messages stream ended without [DONE]", "model", model)
	}

	return &CopilotForwardResult{
		StatusCode:       http.StatusOK,
		Model:            model,
		Usage:            usage,
		FirstTokenMs:     firstTokenMs,
		UpstreamEndpoint: "/chat/completions",
	}, nil
}

// detectAnthropicStream checks if an Anthropic request body has "stream": true.
func detectAnthropicStream(body []byte) bool {
	var req struct {
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	return req.Stream
}

// extractEventType reads the "type" field from a JSON event object for the SSE
// event name (e.g. "message_start", "content_block_delta", …).
func extractEventType(jsonStr string) string {
	var e struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &e); err == nil && e.Type != "" {
		return e.Type
	}
	return "message"
}

// copilotInternalUserResponse is the raw response from the GitHub
// copilot_internal/user endpoint. Only the fields we need are decoded.
type copilotInternalUserResponse struct {
	// CopilotPlan is the plan type string returned by GitHub.
	CopilotPlan string `json:"copilot_plan"`

	// ChatEnabled indicates whether chat is available.
	ChatEnabled bool `json:"chat_enabled"`

	// CopilotQuotaDetails contains fine-grained quota information when available.
	CopilotQuotaDetails *struct {
		Completions         *copilotQuotaDetailRaw `json:"completions"`
		Chat                *copilotQuotaDetailRaw `json:"chat"`
		PremiumInteractions *copilotQuotaDetailRaw `json:"premium_interactions"`
		QuotaResetDate      string                 `json:"quota_reset_date,omitempty"`
	} `json:"copilot_quota_details"`
}

type copilotQuotaDetailRaw struct {
	Entitlement      int  `json:"entitlement,omitempty"`
	OveragePermitted bool `json:"overage_permitted,omitempty"`
	Used             int  `json:"used,omitempty"`
}

// FetchQuota fetches the Copilot quota and plan information for an account from
// the GitHub copilot_internal/user API endpoint.
func (s *CopilotGatewayService) FetchQuota(
	ctx context.Context,
	account *Account,
) (*copilot.CopilotQuotaInfo, error) {
	githubToken := account.GetCredential("github_token")
	if githubToken == "" {
		return nil, fmt.Errorf("copilot: no github_token configured for account %d", account.ID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/copilot_internal/user", nil)
	if err != nil {
		return nil, fmt.Errorf("copilot: build quota request: %w", err)
	}
	req.Header.Set("Authorization", "token "+githubToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-GitHub-Api-Version", copilot.DefaultGitHubAPIVersion)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("copilot: quota request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("copilot: read quota response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("copilot: quota HTTP %d: %s", resp.StatusCode, string(body))
	}

	var raw copilotInternalUserResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("copilot: parse quota response: %w", err)
	}

	// Map plan string to human-readable plan type.
	planType := planTypeFromString(raw.CopilotPlan)

	info := &copilot.CopilotQuotaInfo{
		Plan:     raw.CopilotPlan,
		PlanType: planType,
	}

	if d := raw.CopilotQuotaDetails; d != nil {
		info.QuotaResetDate = d.QuotaResetDate
		if d.Completions != nil {
			info.Completions = &copilot.QuotaDetail{
				Entitlement:      d.Completions.Entitlement,
				OveragePermitted: d.Completions.OveragePermitted,
				Used:             d.Completions.Used,
			}
		}
		if d.Chat != nil {
			info.Chat = &copilot.QuotaDetail{
				Entitlement:      d.Chat.Entitlement,
				OveragePermitted: d.Chat.OveragePermitted,
				Used:             d.Chat.Used,
			}
		}
		if d.PremiumInteractions != nil {
			info.PremiumInteractions = &copilot.QuotaDetail{
				Entitlement:      d.PremiumInteractions.Entitlement,
				OveragePermitted: d.PremiumInteractions.OveragePermitted,
				Used:             d.PremiumInteractions.Used,
			}
		}
	}

	return info, nil
}

// planTypeFromString returns a human-readable plan type label.
func planTypeFromString(plan string) string {
	switch plan {
	case "copilot_for_individuals", "copilot_individual":
		return "Individual"
	case "copilot_business":
		return "Business"
	case "copilot_enterprise":
		return "Enterprise"
	default:
		if plan != "" {
			return plan
		}
		return "Unknown"
	}
}
