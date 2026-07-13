package handler

import (
	"errors"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const copilotMonthlyQuotaExceededReason = "copilot_monthly_quota_exceeded"

// syncCopilotBillingGuardFromFailover mirrors a confirmed upstream Copilot
// monthly-quota failure into the request-local billing guard cache. The
// durable temporary-unschedulable state is persisted by CopilotGatewayService;
// this cache mark prevents another request from selecting the account before
// the scheduler snapshot observes that state.
func syncCopilotBillingGuardFromFailover(account *service.Account, err error) bool {
	var failoverErr *service.UpstreamFailoverError
	if !errors.As(err, &failoverErr) || failoverErr == nil {
		return false
	}
	if failoverErr.StatusCode != http.StatusPaymentRequired ||
		failoverErr.TempUnschedulableReason != copilotMonthlyQuotaExceededReason {
		return false
	}
	return markCopilotBillingGuardExhausted(account)
}
