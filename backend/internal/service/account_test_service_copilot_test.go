package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractCopilotChatContent(t *testing.T) {
	t.Run("message content", func(t *testing.T) {
		content, err := extractCopilotChatContent([]byte(`{"choices":[{"message":{"content":"hello"}}]}`))

		require.NoError(t, err)
		require.Equal(t, "hello", content)
	})

	t.Run("usage only verifies access", func(t *testing.T) {
		content, err := extractCopilotChatContent([]byte(`{"choices":[],"usage":{"prompt_tokens":8,"completion_tokens":100,"total_tokens":108}}`))

		require.NoError(t, err)
		require.Contains(t, content, "model access verified")
	})

	t.Run("empty choices without usage fails", func(t *testing.T) {
		_, err := extractCopilotChatContent([]byte(`{"choices":[]}`))

		require.Error(t, err)
		require.Contains(t, err.Error(), "no message content")
	})
}
