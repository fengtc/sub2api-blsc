package handler

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestCopilotForwardResultHelpersPreserveFirstTokenMs(t *testing.T) {
	firstTokenMs := 123
	result := &service.CopilotForwardResult{
		Model:        "claude-sonnet-5",
		FirstTokenMs: &firstTokenMs,
	}

	messagesResult := copilotMessagesForwardResult(result, true, time.Second)
	if messagesResult.FirstTokenMs == nil || *messagesResult.FirstTokenMs != firstTokenMs {
		t.Fatalf("messages FirstTokenMs = %#v, want %d", messagesResult.FirstTokenMs, firstTokenMs)
	}

	chatResult := copilotChatForwardResult(result, true, time.Second)
	if chatResult.FirstTokenMs == nil || *chatResult.FirstTokenMs != firstTokenMs {
		t.Fatalf("chat FirstTokenMs = %#v, want %d", chatResult.FirstTokenMs, firstTokenMs)
	}
}

func TestCopilotForwardResultHelpersPreserveCacheBreakdown(t *testing.T) {
	result := &service.CopilotForwardResult{
		Model: "claude-sonnet-5",
		Usage: &service.CopilotUsage{
			PromptTokens:             100,
			CompletionTokens:         7,
			TotalTokens:              107,
			CacheCreationInputTokens: 20,
			CacheReadInputTokens:     30,
		},
	}

	messagesResult := copilotMessagesForwardResult(result, true, time.Second)
	if messagesResult.Usage.InputTokens != 50 ||
		messagesResult.Usage.OutputTokens != 7 ||
		messagesResult.Usage.CacheCreationInputTokens != 20 ||
		messagesResult.Usage.CacheReadInputTokens != 30 {
		t.Fatalf("unexpected messages usage: %#v", messagesResult.Usage)
	}

	chatResult := copilotChatForwardResult(result, true, time.Second)
	if chatResult.Usage.InputTokens != 100 ||
		chatResult.Usage.OutputTokens != 7 ||
		chatResult.Usage.CacheCreationInputTokens != 20 ||
		chatResult.Usage.CacheReadInputTokens != 30 {
		t.Fatalf("unexpected chat usage: %#v", chatResult.Usage)
	}
}
