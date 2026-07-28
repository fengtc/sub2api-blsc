package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPollCopilotDeviceAuthorizationHonorsServerPollingInterval(t *testing.T) {
	gin.SetMode(gin.TestMode)
	flowID := strings.Repeat("a", 48)
	copilotDeviceAuthorizationSessions.Store(flowID, &copilotDeviceAuthorizationSession{
		deviceCode: "device-code",
		interval:   5 * time.Second,
		expiresAt:  time.Now().Add(time.Minute),
		nextPollAt: time.Now().Add(4 * time.Second),
	})
	t.Cleanup(func() { copilotDeviceAuthorizationSessions.Delete(flowID) })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/accounts/copilot-device/poll",
		strings.NewReader(`{"flow_id":"`+flowID+`"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	(&AccountHandler{}).PollCopilotDeviceAuthorization(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"status":"pending"`)
	require.Contains(t, recorder.Body.String(), `"retry_after":`)
}

func TestPollCopilotDeviceAuthorizationRejectsExpiredFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	flowID := strings.Repeat("b", 48)
	copilotDeviceAuthorizationSessions.Store(flowID, &copilotDeviceAuthorizationSession{
		deviceCode: "device-code",
		interval:   5 * time.Second,
		expiresAt:  time.Now().Add(-time.Second),
	})
	t.Cleanup(func() { copilotDeviceAuthorizationSessions.Delete(flowID) })

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/accounts/copilot-device/poll",
		strings.NewReader(`{"flow_id":"`+flowID+`"}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")

	(&AccountHandler{}).PollCopilotDeviceAuthorization(ctx)

	require.Equal(t, http.StatusGone, recorder.Code)
	_, exists := copilotDeviceAuthorizationSessions.Load(flowID)
	require.False(t, exists)
}
