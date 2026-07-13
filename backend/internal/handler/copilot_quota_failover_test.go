package handler

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestSyncCopilotBillingGuardFromFailover(t *testing.T) {
	t.Run("matching wrapped quota failover", func(t *testing.T) {
		account := newCopilotQuotaFailoverTestAccount(92001)
		cacheKey := copilotBillingGuardCacheKey(account.ID, "billing-token", time.Now())
		copilotBillingGuardCache.Delete(cacheKey)
		t.Cleanup(func() { copilotBillingGuardCache.Delete(cacheKey) })

		err := fmt.Errorf("forward failed: %w", &service.UpstreamFailoverError{
			StatusCode:              http.StatusPaymentRequired,
			TempUnschedulableReason: copilotMonthlyQuotaExceededReason,
		})
		if !syncCopilotBillingGuardFromFailover(account, err) {
			t.Fatal("matching Copilot quota failover did not mark the billing guard")
		}

		cached, ok := copilotBillingGuardCache.Load(cacheKey)
		if !ok {
			t.Fatal("expected exhausted billing guard cache entry")
		}
		entry := cached.(copilotBillingGuardCacheEntry)
		want := copilotBillingGuardStopLimit(copilotBillingCreditLimit(account))
		if entry.usedCredits != want {
			t.Fatalf("cached used credits = %v, want %v", entry.usedCredits, want)
		}
	})

	t.Run("nonmatching errors", func(t *testing.T) {
		tests := []struct {
			name string
			err  error
		}{
			{
				name: "different reason",
				err: &service.UpstreamFailoverError{
					StatusCode:              http.StatusPaymentRequired,
					TempUnschedulableReason: "oauth_401",
				},
			},
			{
				name: "different status",
				err: &service.UpstreamFailoverError{
					StatusCode:              http.StatusTooManyRequests,
					TempUnschedulableReason: copilotMonthlyQuotaExceededReason,
				},
			},
			{name: "ordinary error", err: errors.New("network failure")},
		}

		for i, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				account := newCopilotQuotaFailoverTestAccount(int64(92010 + i))
				cacheKey := copilotBillingGuardCacheKey(account.ID, "billing-token", time.Now())
				copilotBillingGuardCache.Delete(cacheKey)
				t.Cleanup(func() { copilotBillingGuardCache.Delete(cacheKey) })

				if syncCopilotBillingGuardFromFailover(account, tt.err) {
					t.Fatal("nonmatching error marked the billing guard")
				}
				if _, ok := copilotBillingGuardCache.Load(cacheKey); ok {
					t.Fatal("nonmatching error created a billing guard cache entry")
				}
			})
		}
	})

	t.Run("nil error", func(t *testing.T) {
		account := newCopilotQuotaFailoverTestAccount(92020)
		if syncCopilotBillingGuardFromFailover(account, nil) {
			t.Fatal("nil error marked the billing guard")
		}
	})
}

func newCopilotQuotaFailoverTestAccount(id int64) *service.Account {
	return &service.Account{
		ID:       id,
		Platform: service.PlatformCopilot,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"billing_pat": "billing-token",
		},
		Extra: map[string]any{
			"billing_credit_limit": 20000.0,
		},
	}
}
