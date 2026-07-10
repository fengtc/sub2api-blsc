package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const defaultCopilotBillingCreditLimit = 20000.0

type copilotBillingGuardUsageResponse struct {
	User       string                         `json:"user,omitempty"`
	UsageItems []copilotBillingGuardUsageItem `json:"usageItems"`
}

type copilotBillingGuardUsageItem struct {
	GrossQuantity float64 `json:"grossQuantity"`
}

type copilotBillingGuardCacheEntry struct {
	usedCredits float64
	expiresAt   time.Time
}

var copilotBillingGuardCache sync.Map

func shouldSkipCopilotAccountForBilling(ctx context.Context, account *service.Account) (bool, float64, float64) {
	if account == nil || account.Platform != service.PlatformCopilot || account.Type != service.AccountTypeAPIKey {
		return false, 0, 0
	}
	if copilotBillingAutoPauseDisabled(account) {
		return false, 0, 0
	}
	username, token := copilotBillingGuardCredentials(account)
	if username == "" || token == "" {
		return false, 0, 0
	}
	limit := copilotBillingCreditLimit(account)
	if limit <= 0 {
		return false, 0, 0
	}

	now := time.Now().UTC()
	period := fmt.Sprintf("%04d-%02d", now.Year(), int(now.Month()))
	tokenHash := sha256.Sum256([]byte(token))
	cacheKey := fmt.Sprintf("%d:%s:%x", account.ID, period, tokenHash[:8])
	if cached, ok := copilotBillingGuardCache.Load(cacheKey); ok {
		entry, _ := cached.(copilotBillingGuardCacheEntry)
		if now.Before(entry.expiresAt) {
			return entry.usedCredits >= limit, entry.usedCredits, limit
		}
		copilotBillingGuardCache.Delete(cacheKey)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	used, err := fetchCopilotBillingGuardUsedCredits(fetchCtx, username, token, now.Year(), int(now.Month()))
	if err != nil {
		return false, 0, limit
	}

	ttl := 10 * time.Minute
	if used >= limit {
		ttl = 30 * time.Minute
	}
	copilotBillingGuardCache.Store(cacheKey, copilotBillingGuardCacheEntry{
		usedCredits: used,
		expiresAt:   now.Add(ttl),
	})
	return used >= limit, used, limit
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
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	default:
		return 0
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
	defer resp.Body.Close()
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
