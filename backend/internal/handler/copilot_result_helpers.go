package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func copilotClaudeUsage(usage *service.CopilotUsage) service.ClaudeUsage {
	if usage == nil {
		return service.ClaudeUsage{}
	}
	return service.ClaudeUsage{
		InputTokens:              usage.UncachedPromptTokens(),
		OutputTokens:             usage.CompletionTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
	}
}

func copilotMessagesForwardResult(result *service.CopilotForwardResult, stream bool, duration time.Duration) *service.ForwardResult {
	if result == nil {
		return nil
	}
	out := &service.ForwardResult{
		Model:         result.Model,
		UpstreamModel: result.Model,
		Stream:        stream,
		Duration:      duration,
		FirstTokenMs:  result.FirstTokenMs,
	}
	if result.Usage != nil {
		out.Usage = copilotClaudeUsage(result.Usage)
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
		FirstTokenMs:  result.FirstTokenMs,
	}
	if result.Usage != nil {
		out.Usage = service.OpenAIUsage{
			InputTokens:              result.Usage.PromptTokens,
			OutputTokens:             result.Usage.CompletionTokens,
			CacheCreationInputTokens: result.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     result.Usage.CacheReadInputTokens,
		}
	}
	return out
}
