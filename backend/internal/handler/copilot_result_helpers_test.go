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
