package admin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/copilot"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type ValidateCopilotBillingPATRequest struct {
	Username string `json:"username" binding:"required"`
	Token    string `json:"token" binding:"required"`
}

type githubBillingUsageResponse struct {
	TimePeriod map[string]any           `json:"timePeriod,omitempty"`
	User       string                   `json:"user,omitempty"`
	UsageItems []githubBillingUsageItem `json:"usageItems"`
}

type githubBillingUsageItem struct {
	Product          string  `json:"product"`
	SKU              string  `json:"sku"`
	Model            string  `json:"model"`
	UnitType         string  `json:"unitType"`
	PricePerUnit     float64 `json:"pricePerUnit"`
	GrossQuantity    float64 `json:"grossQuantity"`
	GrossAmount      float64 `json:"grossAmount"`
	DiscountQuantity float64 `json:"discountQuantity"`
	DiscountAmount   float64 `json:"discountAmount"`
	NetQuantity      float64 `json:"netQuantity"`
	NetAmount        float64 `json:"netAmount"`
}

type copilotBillingUsageSnapshot struct {
	Username      string  `json:"username"`
	Period        string  `json:"period"`
	ItemsCount    int     `json:"items_count"`
	GrossQuantity float64 `json:"gross_quantity"`
	GrossAmount   float64 `json:"gross_amount"`
	NetQuantity   float64 `json:"net_quantity"`
	NetAmount     float64 `json:"net_amount"`
	FetchedAt     string  `json:"fetched_at"`
}

type copilotBillingUsageCacheEntry struct {
	snapshot  *copilotBillingUsageSnapshot
	expiresAt time.Time
}

var copilotBillingUsageCache sync.Map

// ValidateCopilotBillingPAT checks whether a fine-grained GitHub PAT can read
// the configured user's Copilot billing usage.
func (h *AccountHandler) ValidateCopilotBillingPAT(c *gin.Context) {
	var req ValidateCopilotBillingPATRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	username := strings.TrimSpace(req.Username)
	token := strings.TrimSpace(req.Token)
	if username == "" || token == "" {
		response.BadRequest(c, "GitHub username and Billing PAT are required")
		return
	}
	if len(username) > 100 || strings.ContainsAny(username, "/?#") {
		response.BadRequest(c, "Invalid GitHub username")
		return
	}

	now := time.Now().UTC()
	usage, statusCode, message, err := fetchGitHubBillingAIUsage(c.Request.Context(), username, token, now.Year(), int(now.Month()), 0)
	if err != nil {
		switch statusCode {
		case http.StatusUnauthorized:
			response.Error(c, http.StatusUnauthorized, "GitHub PAT is invalid or expired")
		case http.StatusForbidden:
			response.Error(c, http.StatusForbidden, "GitHub PAT cannot read billing usage; enable Account permissions -> Plan -> Read-only")
		case http.StatusNotFound:
			response.Error(c, http.StatusNotFound, "GitHub user billing usage was not found")
		default:
			if message != "" {
				response.BadRequest(c, message)
			} else {
				response.ErrorFrom(c, err)
			}
		}
		return
	}

	var grossQuantity, grossAmount, netAmount float64
	for _, item := range usage.UsageItems {
		grossQuantity += item.GrossQuantity
		grossAmount += item.GrossAmount
		netAmount += item.NetAmount
	}
	response.Success(c, gin.H{
		"valid": true, "username": usage.User, "period": usage.TimePeriod,
		"items_count": len(usage.UsageItems), "gross_quantity": grossQuantity,
		"gross_amount": grossAmount, "net_amount": netAmount,
	})
}

func fetchGitHubBillingAIUsage(ctx context.Context, username, token string, year, month, day int) (*githubBillingUsageResponse, int, string, error) {
	endpoint := fmt.Sprintf("https://api.github.com/users/%s/settings/billing/ai_credit/usage?year=%d&month=%d", url.PathEscape(username), year, month)
	if day > 0 {
		endpoint += fmt.Sprintf("&day=%d", day)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "sub2api-billing-check")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var payload githubBillingUsageResponse
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return nil, resp.StatusCode, "", err
		}
		return &payload, resp.StatusCode, "", nil
	}
	var errPayload struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&errPayload)
	if errPayload.Message == "" {
		errPayload.Message = resp.Status
	}
	return nil, resp.StatusCode, errPayload.Message, fmt.Errorf("github billing usage check failed: %s", errPayload.Message)
}

func (h *AccountHandler) getCopilotBillingUsageSnapshot(ctx context.Context, account *service.Account) *copilotBillingUsageSnapshot {
	if account == nil || account.Platform != service.PlatformCopilot || account.Type != service.AccountTypeAPIKey {
		return nil
	}
	username, token := copilotBillingCredentials(account)
	if username == "" || token == "" {
		return nil
	}
	now := time.Now().UTC()
	period := fmt.Sprintf("%04d-%02d", now.Year(), int(now.Month()))
	tokenHash := sha256.Sum256([]byte(token))
	cacheKey := fmt.Sprintf("%d:%s:%x", account.ID, period, tokenHash[:8])
	if cached, ok := copilotBillingUsageCache.Load(cacheKey); ok {
		entry, _ := cached.(copilotBillingUsageCacheEntry)
		if entry.snapshot != nil && now.Before(entry.expiresAt) {
			return entry.snapshot
		}
		copilotBillingUsageCache.Delete(cacheKey)
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	usage, _, _, err := fetchGitHubBillingAIUsage(fetchCtx, username, token, now.Year(), int(now.Month()), 0)
	if err != nil || usage == nil {
		slog.Debug("copilot_billing_usage_fetch_failed", "account_id", account.ID, "username", username, "error", err)
		return nil
	}
	snapshot := summarizeCopilotBillingUsage(usage, username, period, now)
	copilotBillingUsageCache.Store(cacheKey, copilotBillingUsageCacheEntry{snapshot: snapshot, expiresAt: now.Add(10 * time.Minute)})
	return snapshot
}

func copilotBillingCredentials(account *service.Account) (string, string) {
	if account == nil || account.Credentials == nil {
		return "", ""
	}
	username, _ := account.Credentials["billing_username"].(string)
	token, _ := account.Credentials["billing_pat"].(string)
	return strings.TrimSpace(username), strings.TrimSpace(token)
}

func summarizeCopilotBillingUsage(usage *githubBillingUsageResponse, fallbackUsername, period string, fetchedAt time.Time) *copilotBillingUsageSnapshot {
	if usage == nil {
		return nil
	}
	username := strings.TrimSpace(usage.User)
	if username == "" {
		username = fallbackUsername
	}
	snapshot := &copilotBillingUsageSnapshot{Username: username, Period: period, ItemsCount: len(usage.UsageItems), FetchedAt: fetchedAt.Format(time.RFC3339)}
	for _, item := range usage.UsageItems {
		snapshot.GrossQuantity += item.GrossQuantity
		snapshot.GrossAmount += item.GrossAmount
		snapshot.NetQuantity += item.NetQuantity
		snapshot.NetAmount += item.NetAmount
	}
	return snapshot
}

func copilotAvailableModels(account *service.Account) []copilot.Model {
	mapping := account.GetModelMapping()
	if len(mapping) == 0 {
		return copilot.DefaultModels
	}
	defaultByID := make(map[string]copilot.Model, len(copilot.DefaultModels))
	for _, model := range copilot.DefaultModels {
		defaultByID[model.ID] = model
	}
	ids := make([]string, 0, len(mapping))
	for requestedModel := range mapping {
		ids = append(ids, requestedModel)
	}
	sort.Strings(ids)
	models := make([]copilot.Model, 0, len(ids))
	for _, id := range ids {
		if model, ok := defaultByID[id]; ok {
			models = append(models, model)
		} else {
			models = append(models, copilot.Model{ID: id, Object: "model", Type: "model", DisplayName: id})
		}
	}
	return models
}

// GetCopilotQuota fetches quota information for a Copilot account.
func (h *AccountHandler) GetCopilotQuota(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.NotFound(c, "Account not found")
		return
	}
	if account.Platform != service.PlatformCopilot {
		response.BadRequest(c, "Account is not a Copilot account")
		return
	}
	if h.copilotGatewayService == nil {
		response.InternalError(c, "Copilot gateway service not available")
		return
	}
	quotaInfo, err := h.copilotGatewayService.FetchQuota(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, quotaInfo)
}
