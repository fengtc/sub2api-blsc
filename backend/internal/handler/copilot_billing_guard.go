package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	defaultCopilotBillingCreditLimit  = 20000.0
	defaultCopilotBillingSafetyMargin = 200.0

	copilotBillingNormalCacheTTL       = 10 * time.Minute
	copilotBillingNearLimitCacheTTL    = time.Minute
	copilotBillingExhaustedCacheTTL    = 30 * time.Minute
	copilotBillingFetchFailureRetryTTL = time.Minute
)

type copilotBillingGuardUsageResponse struct {
	User       string                         `json:"user,omitempty"`
	UsageItems []copilotBillingGuardUsageItem `json:"usageItems"`
}

type copilotBillingGuardUsageItem struct {
	GrossQuantity float64 `json:"grossQuantity"`
}

type copilotBillingGuardCacheEntry struct {
	usedCredits   float64
	expiresAt     time.Time
	forceSkip     bool
	authoritative bool
}

var copilotBillingGuardCache sync.Map

func shouldSkipCopilotAccountForBilling(ctx context.Context, account *service.Account) (bool, float64, float64) {
	return shouldSkipCopilotAccountForBillingWithFetcher(ctx, account, fetchCopilotBillingGuardUsedCredits)
}

type copilotBillingGuardFetcher func(context.Context, string, string, int, int) (float64, error)

func shouldSkipCopilotAccountForBillingWithFetcher(ctx context.Context, account *service.Account, fetch copilotBillingGuardFetcher) (bool, float64, float64) {
	if account == nil || account.Platform != service.PlatformCopilot || account.Type != service.AccountTypeAPIKey {
		return false, 0, 0
	}
	username, token := copilotBillingGuardCredentials(account)
	if token == "" {
		return false, 0, 0
	}
	configuredLimit := copilotBillingCreditLimit(account)
	if configuredLimit <= 0 {
		return false, 0, 0
	}
	safetyMargin := copilotBillingSafetyMargin(account, configuredLimit)
	stopLimit := copilotBillingGuardStopLimitForAccount(account, configuredLimit)

	now := time.Now().UTC()
	cacheKey := copilotBillingGuardCacheKey(account.ID, token, now)
	var lastGood *copilotBillingGuardCacheEntry
	if cached, ok := copilotBillingGuardCache.Load(cacheKey); ok {
		entry, valid := cached.(copilotBillingGuardCacheEntry)
		if valid {
			lastGood = &entry
			if now.Before(entry.expiresAt) {
				if entry.authoritative || !copilotBillingAutoPauseDisabled(account) {
					return entry.forceSkip || entry.usedCredits >= stopLimit, entry.usedCredits, stopLimit
				}
				return false, entry.usedCredits, stopLimit
			}
		} else {
			copilotBillingGuardCache.Delete(cacheKey)
		}
	}
	// The switch only disables proactive Billing API checks. An authoritative
	// upstream 402 cached above still wins.
	if copilotBillingAutoPauseDisabled(account) {
		return false, 0, stopLimit
	}
	if username == "" {
		return false, 0, stopLimit
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	used, err := fetch(fetchCtx, username, token, now.Year(), int(now.Month()))
	if err != nil {
		slog.Warn("copilot_billing_guard_fetch_failed", "account_id", account.ID, "error", err)
		if lastGood != nil {
			// Keep serving the last successful observation and retry soon. In
			// particular, never turn an exhausted account back on just because
			// GitHub Billing is temporarily unavailable.
			lastGood.expiresAt = now.Add(copilotBillingFetchFailureRetryTTL)
			if copilotBillingGuardNearLimit(lastGood.usedCredits, stopLimit, safetyMargin) {
				lastGood.forceSkip = true
			}
			copilotBillingGuardCache.Store(cacheKey, *lastGood)
			return lastGood.forceSkip || lastGood.usedCredits >= stopLimit, lastGood.usedCredits, stopLimit
		}
		return false, 0, stopLimit
	}

	ttl := copilotBillingGuardCacheTTL(used, stopLimit, safetyMargin)
	copilotBillingGuardCache.Store(cacheKey, copilotBillingGuardCacheEntry{
		usedCredits: used,
		expiresAt:   now.Add(ttl),
	})
	return used >= stopLimit, used, stopLimit
}

// markCopilotBillingGuardExhausted lets an upstream 402/quota_exceeded
// response trip the local guard immediately, without waiting for the next
// GitHub Billing refresh.
func markCopilotBillingGuardExhausted(account *service.Account) bool {
	if account == nil || account.Platform != service.PlatformCopilot || account.Type != service.AccountTypeAPIKey {
		return false
	}
	_, token := copilotBillingGuardCredentials(account)
	if token == "" {
		return false
	}
	configuredLimit := copilotBillingCreditLimit(account)
	if configuredLimit <= 0 {
		return false
	}
	stopLimit := copilotBillingGuardStopLimitForAccount(account, configuredLimit)
	now := time.Now().UTC()
	copilotBillingGuardCache.Store(copilotBillingGuardCacheKey(account.ID, token, now), copilotBillingGuardCacheEntry{
		usedCredits:   stopLimit,
		expiresAt:     copilotBillingGuardAuthoritativeExpiry(now),
		forceSkip:     true,
		authoritative: true,
	})
	return true
}

func copilotBillingGuardCacheKey(accountID int64, token string, now time.Time) string {
	utcNow := now.UTC()
	period := fmt.Sprintf("%04d-%02d", utcNow.Year(), int(utcNow.Month()))
	tokenHash := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%d:%s:%x", accountID, period, tokenHash[:8])
}

func copilotBillingGuardStopLimit(configuredLimit float64) float64 {
	return copilotBillingGuardStopLimitWithMargin(configuredLimit, defaultCopilotBillingSafetyMargin)
}

func copilotBillingGuardStopLimitForAccount(account *service.Account, configuredLimit float64) float64 {
	return copilotBillingGuardStopLimitWithMargin(configuredLimit, copilotBillingSafetyMargin(account, configuredLimit))
}

func copilotBillingGuardStopLimitWithMargin(configuredLimit, safetyMargin float64) float64 {
	if configuredLimit <= 0 {
		return 0
	}
	if safetyMargin < 0 {
		safetyMargin = 0
	}
	if safetyMargin > configuredLimit {
		safetyMargin = configuredLimit
	}
	return configuredLimit - safetyMargin
}

func copilotBillingGuardCacheTTL(used, stopLimit, safetyMargin float64) time.Duration {
	if used >= stopLimit {
		return copilotBillingExhaustedCacheTTL
	}
	if copilotBillingGuardNearLimit(used, stopLimit, safetyMargin) {
		return copilotBillingNearLimitCacheTTL
	}
	return copilotBillingNormalCacheTTL
}

func copilotBillingGuardNearLimit(used, stopLimit, safetyMargin float64) bool {
	return safetyMargin > 0 && used >= stopLimit-safetyMargin
}

func copilotBillingGuardAuthoritativeExpiry(now time.Time) time.Time {
	utcNow := now.UTC()
	return time.Date(utcNow.Year(), utcNow.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

func copilotBillingGuardCredentials(account *service.Account) (string, string) {
	if account == nil || account.Credentials == nil {
		return "", ""
	}
	username, _ := account.Credentials["billing_username"].(string)
	token, _ := account.Credentials["billing_pat"].(string)
	return strings.TrimSpace(username), strings.TrimSpace(token)
}

func copilotBillingCreditLimit(account *service.Account) float64 {
	if account == nil || account.Extra == nil {
		return defaultCopilotBillingCreditLimit
	}
	if v, ok := account.Extra["billing_credit_limit"]; ok {
		if n := parseCopilotBillingFloat(v); n > 0 {
			return n
		}
	}
	return defaultCopilotBillingCreditLimit
}

// copilotBillingSafetyMargin returns an absolute AI-credit margin. An
// explicitly configured zero disables proactive headroom while leaving the
// authoritative upstream-402 guard intact.
func copilotBillingSafetyMargin(account *service.Account, configuredLimit float64) float64 {
	margin := defaultCopilotBillingSafetyMargin
	if account != nil && account.Extra != nil {
		if value, exists := account.Extra["billing_safety_margin"]; exists {
			if parsed, valid := parseCopilotBillingFloatOK(value); valid && parsed >= 0 {
				margin = parsed
			}
		}
	}
	if margin > configuredLimit {
		return configuredLimit
	}
	return margin
}

func copilotBillingAutoPauseDisabled(account *service.Account) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	if v, ok := account.Extra["billing_auto_pause_disabled"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func parseCopilotBillingFloat(value any) float64 {
	n, _ := parseCopilotBillingFloatOK(value)
	return n
}

func parseCopilotBillingFloatOK(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func fetchCopilotBillingGuardUsedCredits(ctx context.Context, username, token string, year, month int) (float64, error) {
	endpoint := fmt.Sprintf("https://api.github.com/users/%s/settings/billing/ai_credit/usage?year=%d&month=%d", url.PathEscape(strings.TrimSpace(username)), year, month)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "sub2api-billing-guard")

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("github billing usage returned status %d", resp.StatusCode)
	}

	var payload copilotBillingGuardUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	var used float64
	for _, item := range payload.UsageItems {
		used += item.GrossQuantity
	}
	return used, nil
}
