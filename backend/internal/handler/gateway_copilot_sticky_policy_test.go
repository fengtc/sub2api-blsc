package handler

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestShouldBindStickyAfterWait(t *testing.T) {
	t.Run("normal wait binds selected account", func(t *testing.T) {
		selection := &service.AccountSelectionResult{}
		require.True(t, shouldBindStickyAfterWait(selection))
	})

	t.Run("copilot overflow wait preserves original binding", func(t *testing.T) {
		selection := &service.AccountSelectionResult{PreserveStickyBinding: true}
		require.False(t, shouldBindStickyAfterWait(selection))
	})

	t.Run("nil selection never binds", func(t *testing.T) {
		require.False(t, shouldBindStickyAfterWait(nil))
	})
}

func TestShouldRetryCopilotOverflowWaitQueueFull(t *testing.T) {
	t.Run("copilot account retries even without an existing sticky overflow", func(t *testing.T) {
		account := &service.Account{Platform: service.PlatformCopilot}
		require.True(t, shouldRetryCopilotOverflowWaitQueueFull(account, &service.AccountSelectionResult{}))
	})

	t.Run("mixed overflow account retries while preserving copilot sticky", func(t *testing.T) {
		account := &service.Account{Platform: service.PlatformAnthropic}
		selection := &service.AccountSelectionResult{PreserveStickyBinding: true}
		require.True(t, shouldRetryCopilotOverflowWaitQueueFull(account, selection))
	})

	t.Run("ordinary non copilot queue full keeps the existing rejection", func(t *testing.T) {
		account := &service.Account{Platform: service.PlatformAnthropic}
		require.False(t, shouldRetryCopilotOverflowWaitQueueFull(account, &service.AccountSelectionResult{}))
	})
}

func TestRecordCopilotWaitQueueFull(t *testing.T) {
	state := NewFailoverState(3, true)

	recordCopilotWaitQueueFull(state, 42)

	require.Contains(t, state.FailedAccountIDs, int64(42))
	require.NotNil(t, state.LastFailoverErr)
	require.Equal(t, http.StatusTooManyRequests, state.LastFailoverErr.StatusCode)
	require.Equal(t, http.StatusTooManyRequests, state.LastFailoverErr.ClientStatusCode)
	require.Equal(t, service.NextAccountStop, state.LastFailoverErr.NextAccountAction)
	require.Equal(t, FailoverExhausted, state.HandleSelectionExhausted(t.Context()))
}
