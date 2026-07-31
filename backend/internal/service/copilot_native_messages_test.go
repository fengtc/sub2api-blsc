package service

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func TestPrepareCopilotNativeMessagesBodyPreservesCacheBreakpoints(t *testing.T) {
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"metadata":{"user_id":"session-test"},
		"system":[{"type":"text","text":"stable system","cache_control":{"type":"ephemeral","scope":"global"}}],
		"tools":[{"name":"Read","description":"read","input_schema":{"type":"object"}}],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"first"}]},
			{"role":"assistant","content":[{"type":"text","text":"answer"}]},
			{"role":"user","content":[{"type":"text","text":"second"}]},
			{"role":"assistant","content":[{"type":"text","text":"latest"}]}
		],
		"max_tokens":1024,
		"stream":true
	}`)

	got, model, metadataUserID, err := prepareCopilotNativeMessagesBody(body, nil)
	if err != nil {
		t.Fatalf("prepareCopilotNativeMessagesBody error: %v", err)
	}
	if model != "claude-sonnet-4.5" {
		t.Fatalf("model = %q, want claude-sonnet-4.5", model)
	}
	if metadataUserID != "session-test" {
		t.Fatalf("metadata user id = %q, want session-test", metadataUserID)
	}
	if gotModel := gjson.GetBytes(got, "model").String(); gotModel != model {
		t.Fatalf("wire model = %q, want %q", gotModel, model)
	}
	if gjson.GetBytes(got, "system.0.cache_control.scope").Exists() {
		t.Fatalf("unsupported cache_control.scope survived: %s", string(got))
	}
	if gotType := gjson.GetBytes(got, "system.0.cache_control.type").String(); gotType != "ephemeral" {
		t.Fatalf("system cache type = %q, want ephemeral", gotType)
	}
	if gotType := gjson.GetBytes(got, "tools.0.cache_control.type").String(); gotType != "ephemeral" {
		t.Fatalf("tool cache type = %q, want ephemeral", gotType)
	}
	if gotType := gjson.GetBytes(got, "messages.3.content.0.cache_control.type").String(); gotType != "ephemeral" {
		t.Fatalf("latest message cache type = %q, want ephemeral", gotType)
	}
	if count := strings.Count(string(got), `"cache_control"`); count != maxCacheControlBlocks {
		t.Fatalf("cache_control count = %d, want %d\n%s", count, maxCacheControlBlocks, string(got))
	}
}

func TestCopilotNativeMessagesHasVision(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "direct image",
			body: `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"x"}}]}]}`,
			want: true,
		},
		{
			name: "tool result image",
			body: `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"tool_1","content":[{"type":"image","source":{"type":"base64","data":"x"}}]}]}]}`,
			want: true,
		},
		{
			name: "text only",
			body: `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := copilotNativeMessagesHasVision([]byte(tt.body)); got != tt.want {
				t.Fatalf("copilotNativeMessagesHasVision() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCopilotGatewayService_ForwardMessagesUsesNativeEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var upstreamPath string
	var upstreamBody []byte
	var upstreamHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		upstreamHeaders = r.Header.Clone()
		var err error
		upstreamBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"msg_native","type":"message","role":"assistant","model":"claude-sonnet-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":50,"output_tokens":7,"cache_creation_input_tokens":20,"cache_read_input_tokens":30}}`)
	}))
	defer server.Close()

	provider := NewCopilotTokenProvider(nil)
	token := newCopilotTestToken("copilot-token-123")
	provider.mu.Lock()
	provider.tokens[71] = &token
	provider.mu.Unlock()

	svc := NewCopilotGatewayService(provider)
	account := &Account{
		ID:       71,
		Platform: PlatformCopilot,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"github_token": "ghp_test",
			"base_url":     server.URL,
		},
	}
	body := []byte(`{
		"model":"claude-sonnet-5",
		"metadata":{"user_id":"{\"device_id\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"account_uuid\":\"\",\"session_id\":\"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa\"}"},
		"system":[{"type":"text","text":"stable","cache_control":{"type":"ephemeral"}}],
		"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}],
		"max_tokens":64,
		"stream":false
	}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14,unsupported-beta")

	result, err := svc.ForwardMessages(t.Context(), c, account, body)
	if err != nil {
		t.Fatalf("ForwardMessages error: %v", err)
	}
	if upstreamPath != "/v1/messages" {
		t.Fatalf("upstream path = %q, want /v1/messages", upstreamPath)
	}
	if upstreamHeaders.Get("anthropic-beta") != "interleaved-thinking-2025-05-14" {
		t.Fatalf("anthropic-beta = %q", upstreamHeaders.Get("anthropic-beta"))
	}
	if upstreamHeaders.Get("x-interaction-type") != "messages-proxy" ||
		upstreamHeaders.Get("openai-intent") != "messages-proxy" {
		t.Fatalf("unexpected native headers: %#v", upstreamHeaders)
	}
	if upstreamHeaders.Get("copilot-integration-id") != "" {
		t.Fatalf("copilot-integration-id should be removed for native messages")
	}
	if got := gjson.GetBytes(upstreamBody, "system.0.cache_control.type").String(); got != "ephemeral" {
		t.Fatalf("system cache control = %q\n%s", got, string(upstreamBody))
	}
	if result == nil || result.Usage == nil {
		t.Fatalf("missing result usage: %#v", result)
	}
	if result.Usage.PromptTokens != 100 ||
		result.Usage.UncachedPromptTokens() != 50 ||
		result.Usage.CompletionTokens != 7 ||
		result.Usage.CacheCreationInputTokens != 20 ||
		result.Usage.CacheReadInputTokens != 30 {
		t.Fatalf("unexpected native usage: %#v", result.Usage)
	}
	if !strings.Contains(recorder.Body.String(), `"cache_creation_input_tokens":20`) ||
		!strings.Contains(recorder.Body.String(), `"cache_read_input_tokens":30`) {
		t.Fatalf("native response was not passed through: %s", recorder.Body.String())
	}
}

func TestCopilotGatewayService_ForwardMessagesStreamsNativeUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "event: message_start\n")
		fmt.Fprint(w, `data: {"type":"message_start","message":{"id":"msg_native","type":"message","role":"assistant","model":"claude-sonnet-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":50,"output_tokens":1,"cache_creation_input_tokens":20,"cache_read_input_tokens":30}}}`+"\n\n")
		fmt.Fprint(w, "event: content_block_start\n")
		fmt.Fprint(w, `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`+"\n\n")
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`+"\n\n")
		fmt.Fprint(w, "event: message_delta\n")
		fmt.Fprint(w, `data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":7}}`+"\n\n")
		fmt.Fprint(w, "event: message_stop\n")
		fmt.Fprint(w, `data: {"type":"message_stop"}`+"\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewCopilotTokenProvider(nil)
	token := newCopilotTestToken("copilot-token-123")
	provider.mu.Lock()
	provider.tokens[72] = &token
	provider.mu.Unlock()

	svc := NewCopilotGatewayService(provider)
	account := &Account{
		ID:       72,
		Platform: PlatformCopilot,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"github_token": "ghp_test",
			"base_url":     server.URL,
		},
	}
	body := []byte(`{"model":"claude-sonnet-5","messages":[{"role":"user","content":"hello"}],"max_tokens":64,"stream":true}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	result, err := svc.ForwardMessages(t.Context(), c, account, body)
	if err != nil {
		t.Fatalf("ForwardMessages error: %v", err)
	}
	if result == nil || result.Usage == nil {
		t.Fatalf("missing result usage: %#v", result)
	}
	if result.Usage.UncachedPromptTokens() != 50 ||
		result.Usage.CompletionTokens != 7 ||
		result.Usage.CacheCreationInputTokens != 20 ||
		result.Usage.CacheReadInputTokens != 30 {
		t.Fatalf("unexpected streaming native usage: %#v", result.Usage)
	}
	if result.FirstTokenMs == nil {
		t.Fatal("FirstTokenMs is nil")
	}
	bodyText := recorder.Body.String()
	if !strings.Contains(bodyText, "event: message_start") ||
		!strings.Contains(bodyText, `"cache_creation_input_tokens":20`) ||
		!strings.Contains(bodyText, "event: message_stop") {
		t.Fatalf("native SSE was not passed through:\n%s", bodyText)
	}
}
