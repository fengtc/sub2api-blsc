package handler

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestCopilotBillingGuardStopLimitUsesDefaultSafetyMargin(t *testing.T) {
	margin := copilotBillingSafetyMargin(nil, defaultCopilotBillingCreditLimit)
	if margin != 200 {
		t.Fatalf("default safety margin = %v, want 200", margin)
	}
	got := copilotBillingGuardStopLimit(defaultCopilotBillingCreditLimit)
	if got != 19800 {
		t.Fatalf("stop limit = %v, want 19800", got)
	}
}

func TestCopilotBillingGuardSafetyMarginConfiguration(t *testing.T) {
	account := newCopilotBillingGuardTestAccount(91000)
	tests := []struct {
		name     string
		value    any
		want     float64
		wantStop float64
	}{
		{name: "custom absolute credits", value: 350.0, want: 350, wantStop: 19650},
		{name: "explicit zero disables preventive headroom", value: 0, want: 0, wantStop: 20000},
		{name: "negative falls back to default", value: -1, want: 200, wantStop: 19800},
		{name: "invalid falls back to default", value: "100", want: 200, wantStop: 19800},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account.Extra["billing_safety_margin"] = tt.value
			if got := copilotBillingSafetyMargin(account, 20000); got != tt.want {
				t.Fatalf("safety margin = %v, want %v", got, tt.want)
			}
			if got := copilotBillingGuardStopLimitForAccount(account, 20000); got != tt.wantStop {
				t.Fatalf("stop limit = %v, want %v", got, tt.wantStop)
			}
		})
	}
}

func TestCopilotBillingGuardCacheTTL(t *testing.T) {
	stopLimit := 19800.0
	safetyMargin := 200.0
	tests := []struct {
		name string
		used float64
		want time.Duration
	}{
		{name: "normal", used: 19599, want: copilotBillingNormalCacheTTL},
		{name: "near limit lower boundary", used: 19600, want: copilotBillingNearLimitCacheTTL},
		{name: "near limit", used: 19799, want: copilotBillingNearLimitCacheTTL},
		{name: "exhausted", used: 19800, want: copilotBillingExhaustedCacheTTL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := copilotBillingGuardCacheTTL(tt.used, stopLimit, safetyMargin); got != tt.want {
				t.Fatalf("cache TTL = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestShouldSkipCopilotAccountForBillingNearLimitUsesShortCache(t *testing.T) {
	account := newCopilotBillingGuardTestAccount(91001)
	cacheKey := copilotBillingGuardCacheKey(account.ID, "billing-token", time.Now())
	copilotBillingGuardCache.Delete(cacheKey)
	t.Cleanup(func() { copilotBillingGuardCache.Delete(cacheKey) })

	fetchCalls := 0
	fetch := func(context.Context, string, string, int, int) (float64, error) {
		fetchCalls++
		return 19750, nil
	}

	before := time.Now()
	skip, used, limit := shouldSkipCopilotAccountForBillingWithFetcher(context.Background(), account, fetch)
	if skip {
		t.Fatal("near-limit account was skipped before reaching the stop limit")
	}
	if used != 19750 || limit != 19800 {
		t.Fatalf("got used=%v limit=%v, want used=19750 limit=19800", used, limit)
	}
	cached, ok := copilotBillingGuardCache.Load(cacheKey)
	if !ok {
		t.Fatal("expected a cached billing observation")
	}
	entry := cached.(copilotBillingGuardCacheEntry)
	if got := entry.expiresAt.Sub(before); got < 55*time.Second || got > 65*time.Second {
		t.Fatalf("near-limit cache lifetime = %s, want about 60s", got)
	}

	shouldSkipCopilotAccountForBillingWithFetcher(context.Background(), account, fetch)
	if fetchCalls != 1 {
		t.Fatalf("fetch calls = %d, want 1 while near-limit cache is fresh", fetchCalls)
	}
}

func TestShouldSkipCopilotAccountForBillingFetchFailureKeepsNearLimitLastGood(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantInLog string
	}{
		{name: "deadline exceeded", err: context.DeadlineExceeded, wantInLog: "context deadline exceeded"},
		{name: "github 403", err: errors.New("github billing usage returned status 403"), wantInLog: "status 403"},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := newCopilotBillingGuardTestAccount(91002 + int64(i))
			cacheKey := copilotBillingGuardCacheKey(account.ID, "billing-token", time.Now())
			copilotBillingGuardCache.Store(cacheKey, copilotBillingGuardCacheEntry{
				usedCredits: 19750,
				expiresAt:   time.Now().Add(-time.Second),
			})
			defer copilotBillingGuardCache.Delete(cacheKey)

			var logs bytes.Buffer
			previousLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(previousLogger)

			fetchCalls := 0
			fetch := func(context.Context, string, string, int, int) (float64, error) {
				fetchCalls++
				return 0, tt.err
			}

			skip, used, limit := shouldSkipCopilotAccountForBillingWithFetcher(context.Background(), account, fetch)
			if !skip {
				t.Fatal("near-limit last-good observation should conservatively skip after fetch failure")
			}
			if used != 19750 || limit != 19800 {
				t.Fatalf("got used=%v limit=%v, want preserved used=19750 limit=19800", used, limit)
			}
			if !strings.Contains(logs.String(), "level=WARN") ||
				!strings.Contains(logs.String(), "copilot_billing_guard_fetch_failed") ||
				!strings.Contains(logs.String(), tt.wantInLog) {
				t.Fatalf("expected warn log for %s, got %q", tt.name, logs.String())
			}

			cached, ok := copilotBillingGuardCache.Load(cacheKey)
			if !ok {
				t.Fatal("last-good observation was removed after fetch failure")
			}
			entry := cached.(copilotBillingGuardCacheEntry)
			if !entry.forceSkip {
				t.Fatal("near-limit failed refresh was not cached as a conservative skip")
			}
			if remaining := time.Until(entry.expiresAt); remaining < 55*time.Second || remaining > 65*time.Second {
				t.Fatalf("fetch-failure retry cache lifetime = %s, want about 60s", remaining)
			}

			shouldSkipCopilotAccountForBillingWithFetcher(context.Background(), account, fetch)
			if fetchCalls != 1 {
				t.Fatalf("fetch calls = %d, want 1 while failure retry cache is fresh", fetchCalls)
			}
		})
	}
}

func TestMarkCopilotBillingGuardExhaustedOverridesSnapshotAndAutoPause(t *testing.T) {
	account := newCopilotBillingGuardTestAccount(91003)
	delete(account.Credentials, "billing_username")
	account.Extra["billing_auto_pause_disabled"] = true
	cacheKey := copilotBillingGuardCacheKey(account.ID, "billing-token", time.Now())
	// Mirror the observed 19.9k case: even though the snapshot is still below
	// the configured 20,000-credit limit, the upstream 402 is authoritative.
	copilotBillingGuardCache.Store(cacheKey, copilotBillingGuardCacheEntry{
		usedCredits: 19912,
		expiresAt:   time.Now().Add(10 * time.Minute),
	})
	t.Cleanup(func() { copilotBillingGuardCache.Delete(cacheKey) })

	before := time.Now()
	if !markCopilotBillingGuardExhausted(account) {
		t.Fatal("expected upstream 402 helper to mark the account exhausted")
	}
	cached, ok := copilotBillingGuardCache.Load(cacheKey)
	if !ok {
		t.Fatal("expected an exhausted cache entry")
	}
	entry := cached.(copilotBillingGuardCacheEntry)
	if entry.usedCredits != 19800 || !entry.forceSkip || !entry.authoritative {
		t.Fatalf("exhausted used credits = %v, want 19800", entry.usedCredits)
	}
	if want := copilotBillingGuardAuthoritativeExpiry(before); !entry.expiresAt.Equal(want) {
		t.Fatalf("authoritative expiry = %s, want next UTC month %s", entry.expiresAt, want)
	}

	fetchCalls := 0
	skip, used, limit := shouldSkipCopilotAccountForBillingWithFetcher(context.Background(), account, func(context.Context, string, string, int, int) (float64, error) {
		fetchCalls++
		return 0, nil
	})
	if !skip || used != 19800 || limit != 19800 {
		t.Fatalf("got skip=%v used=%v limit=%v, want true/19800/19800", skip, used, limit)
	}
	if fetchCalls != 0 {
		t.Fatalf("fetch calls = %d, want 0 after local exhausted mark", fetchCalls)
	}
}

func newCopilotBillingGuardTestAccount(id int64) *service.Account {
	return &service.Account{
		ID:       id,
		Platform: service.PlatformCopilot,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"billing_username": "billing-user",
			"billing_pat":      "billing-token",
		},
		Extra: map[string]any{
			"billing_credit_limit": 20000.0,
		},
	}
}
