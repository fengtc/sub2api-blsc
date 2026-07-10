package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func copilotMessagesForwardResult(result *service.CopilotForwardResult, stream bool, duration time.Duration) *service.ForwardResult {
	if result == nil {
		return nil
	}
	out := &service.ForwardResult{
		Model:         result.Model,
		UpstreamModel: result.Model,
		Stream:        stream,
		Duration:      duration,
	}
	if result.Usage != nil {
		out.Usage = service.ClaudeUsage{
			InputTokens:  result.Usage.PromptTokens,
			OutputTokens: result.Usage.CompletionTokens,
		}
	}
	return out
}

func copilotChatForwardResult(result *service.CopilotForwardResult, stream bool, duration time.Duration) *service.OpenAIForwardResult {
	if result == nil {
		return nil
	}
	out := &service.OpenAIForwardResult{
		Model:         result.Model,
		UpstreamModel: result.Model,
		Stream:        stream,
		Duration:      duration,
	}
	if result.Usage != nil {
		out.Usage = service.OpenAIUsage{
			InputTokens:  result.Usage.PromptTokens,
			OutputTokens: result.Usage.CompletionTokens,
		}
	}
	return out
}
