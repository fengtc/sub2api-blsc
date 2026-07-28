package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type copilotStickyTestAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (r *copilotStickyTestAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, errors.New("account not found")
}

func (r *copilotStickyTestAccountRepo) listByPlatforms(platforms []string) []Account {
	allowed := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowed[platform] = struct{}{}
	}
	result := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if _, ok := allowed[account.Platform]; ok && account.IsSchedulable() {
			result = append(result, account)
		}
	}
	return result
}

func (r *copilotStickyTestAccountRepo) ListSchedulableByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	return r.listByPlatforms(platforms), nil
}

func (r *copilotStickyTestAccountRepo) ListSchedulableByGroupIDAndPlatforms(_ context.Context, _ int64, platforms []string) ([]Account, error) {
	return r.listByPlatforms(platforms), nil
}

func (r *copilotStickyTestAccountRepo) ListSchedulableUngroupedByPlatforms(_ context.Context, platforms []string) ([]Account, error) {
	return r.listByPlatforms(platforms), nil
}

type copilotStickyTestGroupRepo struct {
	GroupRepository
	group *Group
}

func (r *copilotStickyTestGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	if r.group != nil && r.group.ID == id {
		return r.group, nil
	}
	return nil, ErrGroupNotFound
}

func (r *copilotStickyTestGroupRepo) GetByIDLite(ctx context.Context, id int64) (*Group, error) {
	return r.GetByID(ctx, id)
}

type copilotStickyTestGatewayCache struct {
	GatewayCache
	bindings map[string]int64
	setCalls []int64
}

func (c *copilotStickyTestGatewayCache) GetSessionAccountID(_ context.Context, _ int64, sessionHash string) (int64, error) {
	accountID, ok := c.bindings[sessionHash]
	if !ok {
		return 0, errors.New("session not found")
	}
	return accountID, nil
}

func (c *copilotStickyTestGatewayCache) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, accountID int64, _ time.Duration) error {
	if c.bindings == nil {
		c.bindings = make(map[string]int64)
	}
	c.bindings[sessionHash] = accountID
	c.setCalls = append(c.setCalls, accountID)
	return nil
}

func (c *copilotStickyTestGatewayCache) RefreshSessionTTL(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

func (c *copilotStickyTestGatewayCache) DeleteSessionAccountID(_ context.Context, _ int64, sessionHash string) error {
	delete(c.bindings, sessionHash)
	return nil
}

type copilotStickyTestConcurrencyCache struct {
	ConcurrencyCache

	mu             sync.Mutex
	acquireResults map[int64]bool
	loadMap        map[int64]*AccountLoadInfo
	freshLoadMap   map[int64]*AccountLoadInfo
	loadErr        error
	waitCounts     map[int64]int
	acquireCalls   []int64
	loadBatchCalls int
}

func (c *copilotStickyTestConcurrencyCache) AcquireAccountSlot(_ context.Context, accountID int64, _ int, _ string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acquireCalls = append(c.acquireCalls, accountID)
	if acquired, ok := c.acquireResults[accountID]; ok {
		return acquired, nil
	}
	return true, nil
}

func (c *copilotStickyTestConcurrencyCache) ReleaseAccountSlot(_ context.Context, _ int64, _ string) error {
	return nil
}

func (c *copilotStickyTestConcurrencyCache) GetAccountWaitingCount(_ context.Context, accountID int64) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.waitCounts[accountID], nil
}

func (c *copilotStickyTestConcurrencyCache) GetAccountsLoadBatch(_ context.Context, accounts []AccountWithConcurrency) (map[int64]*AccountLoadInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadBatchCalls++
	if c.loadErr != nil {
		return nil, c.loadErr
	}
	loadMap := c.loadMap
	if c.loadBatchCalls > 1 && c.freshLoadMap != nil {
		loadMap = c.freshLoadMap
	}
	result := make(map[int64]*AccountLoadInfo, len(accounts))
	for _, account := range accounts {
		if load, ok := loadMap[account.ID]; ok {
			copied := *load
			result[account.ID] = &copied
			continue
		}
		result[account.ID] = &AccountLoadInfo{AccountID: account.ID}
	}
	return result, nil
}

func newCopilotStickyTestService(
	accounts []Account,
	cache *copilotStickyTestGatewayCache,
	concurrencyCache *copilotStickyTestConcurrencyCache,
	loadBatchEnabled bool,
	group *Group,
) *GatewayService {
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Gateway: config.GatewayConfig{
			Scheduling: config.GatewaySchedulingConfig{
				LoadBatchEnabled:         loadBatchEnabled,
				StickySessionMaxWaiting:  3,
				StickySessionWaitTimeout: 2 * time.Minute,
				FallbackWaitTimeout:      30 * time.Second,
				FallbackMaxWaiting:       100,
			},
		},
	}
	repo := &copilotStickyTestAccountRepo{accounts: accounts}
	service := &GatewayService{
		accountRepo:        repo,
		cache:              cache,
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(concurrencyCache),
	}
	if group != nil {
		service.groupRepo = &copilotStickyTestGroupRepo{group: group}
	}
	return service
}

func copilotStickyTestAccount(id int64, platform string, priority int) Account {
	return Account{
		ID:          id,
		Platform:    platform,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    priority,
	}
}

func TestGatewayService_CopilotStickyEscape(t *testing.T) {
	const sessionHash = "copilot-sticky-session"

	t.Run("available sticky keeps affinity and skips load lookup", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformCopilot, 1),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: true, 2: true},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, true, nil)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), nil, sessionHash, "", nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.True(t, selection.Acquired)
		require.Nil(t, selection.WaitPlan)
		require.False(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(1), selection.Account.ID)
		require.Equal(t, int64(1), cache.bindings[sessionHash])
		require.Empty(t, cache.setCalls)
		require.Equal(t, 0, concurrencyCache.loadBatchCalls)
		require.Equal(t, []int64{1}, concurrencyCache.acquireCalls)
		selection.ReleaseFunc()
	})

	t.Run("busy sticky spills to free copilot without rebinding", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformCopilot, 1),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			loadMap: map[int64]*AccountLoadInfo{
				1: {AccountID: 1, CurrentConcurrency: 1, LoadRate: 100},
				2: {AccountID: 2, CurrentConcurrency: 0, LoadRate: 0},
			},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, true, nil)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), nil, sessionHash, "", nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.True(t, selection.Acquired)
		require.Nil(t, selection.WaitPlan)
		require.True(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(2), selection.Account.ID)
		require.Equal(t, int64(1), cache.bindings[sessionHash], "overflow must not migrate the original sticky binding")
		require.Empty(t, cache.setCalls)
		require.Equal(t, []int64{1, 2}, concurrencyCache.acquireCalls)
		selection.ReleaseFunc()
	})

	t.Run("all copilot accounts full still returns a wait plan", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 2),
			copilotStickyTestAccount(2, PlatformCopilot, 1),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: false},
			loadMap: map[int64]*AccountLoadInfo{
				1: {AccountID: 1, CurrentConcurrency: 1, LoadRate: 100},
				2: {AccountID: 2, CurrentConcurrency: 1, LoadRate: 100},
			},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, true, nil)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), nil, sessionHash, "", nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.False(t, selection.Acquired)
		require.NotNil(t, selection.WaitPlan)
		require.True(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(2), selection.WaitPlan.AccountID)
		require.Equal(t, int64(1), cache.bindings[sessionHash])
		require.Empty(t, cache.setCalls)
	})

	t.Run("non copilot sticky keeps the existing wait behavior", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformAnthropic, 1),
			copilotStickyTestAccount(2, PlatformAnthropic, 2),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			waitCounts:     map[int64]int{1: 0},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, true, nil)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), nil, sessionHash, "", nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.False(t, selection.Acquired)
		require.NotNil(t, selection.WaitPlan)
		require.False(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(1), selection.WaitPlan.AccountID)
		require.Equal(t, 0, concurrencyCache.loadBatchCalls)
		require.Equal(t, []int64{1}, concurrencyCache.acquireCalls)
	})

	t.Run("model routing uses the same soft escape semantics", func(t *testing.T) {
		const model = "claude-sonnet-4"
		groupID := int64(42)
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformCopilot, 1),
		}
		group := &Group{
			ID:                  groupID,
			Platform:            PlatformAnthropic,
			Status:              StatusActive,
			Hydrated:            true,
			ModelRoutingEnabled: true,
			ModelRouting: map[string][]int64{
				model: {1, 2},
			},
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			loadMap: map[int64]*AccountLoadInfo{
				1: {AccountID: 1, CurrentConcurrency: 1, LoadRate: 100},
				2: {AccountID: 2, CurrentConcurrency: 0, LoadRate: 0},
			},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, true, group)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), &groupID, sessionHash, model, nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.True(t, selection.Acquired)
		require.True(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(2), selection.Account.ID)
		require.Equal(t, int64(1), cache.bindings[sessionHash])
		require.Empty(t, cache.setCalls)
		require.Equal(t, []int64{1, 2}, concurrencyCache.acquireCalls)
		selection.ReleaseFunc()
	})

	t.Run("model routing refreshes stale loads before waiting", func(t *testing.T) {
		const model = "claude-sonnet-4"
		groupID := int64(43)
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformCopilot, 1),
		}
		group := &Group{
			ID:                  groupID,
			Platform:            PlatformAnthropic,
			Status:              StatusActive,
			Hydrated:            true,
			ModelRoutingEnabled: true,
			ModelRouting: map[string][]int64{
				model: {1, 2},
			},
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			loadMap: map[int64]*AccountLoadInfo{
				1: {AccountID: 1, CurrentConcurrency: 1, LoadRate: 100},
				2: {AccountID: 2, CurrentConcurrency: 1, LoadRate: 100},
			},
			freshLoadMap: map[int64]*AccountLoadInfo{
				1: {AccountID: 1, CurrentConcurrency: 1, LoadRate: 100},
				2: {AccountID: 2, CurrentConcurrency: 0, LoadRate: 0},
			},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, true, group)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), &groupID, sessionHash, model, nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.True(t, selection.Acquired)
		require.True(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(2), selection.Account.ID)
		require.Equal(t, int64(1), cache.bindings[sessionHash])
		require.Empty(t, cache.setCalls)
		require.Equal(t, 2, concurrencyCache.loadBatchCalls)
		require.Equal(t, []int64{1, 2}, concurrencyCache.acquireCalls)
		selection.ReleaseFunc()
	})

	t.Run("batch load failure scans backups without rebinding", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformCopilot, 2),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			loadErr:        errors.New("load lookup failed"),
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, true, nil)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), nil, sessionHash, "", nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.True(t, selection.Acquired)
		require.True(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(2), selection.Account.ID)
		require.Equal(t, int64(1), cache.bindings[sessionHash])
		require.Empty(t, cache.setCalls)
		require.Equal(t, []int64{1, 2}, concurrencyCache.acquireCalls)
		selection.ReleaseFunc()
	})

	t.Run("legacy scheduling also scans backups and preserves affinity", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformCopilot, 2),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, false, nil)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), nil, sessionHash, "", nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.True(t, selection.Acquired)
		require.True(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(2), selection.Account.ID)
		require.Equal(t, int64(1), cache.bindings[sessionHash])
		require.Empty(t, cache.setCalls)
		require.Equal(t, []int64{1, 2}, concurrencyCache.acquireCalls)
		selection.ReleaseFunc()
	})

	t.Run("legacy scheduling falls back to waiting when every copilot is full", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformCopilot, 2),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: false},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, false, nil)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), nil, sessionHash, "", nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.False(t, selection.Acquired)
		require.NotNil(t, selection.WaitPlan)
		require.True(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(1), selection.WaitPlan.AccountID)
		require.Equal(t, 30*time.Second, selection.WaitPlan.Timeout)
		require.Equal(t, int64(1), cache.bindings[sessionHash])
		require.Empty(t, cache.setCalls)
	})

	t.Run("stale load snapshot is refreshed before overflow waits", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformCopilot, 1),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: true},
			loadMap: map[int64]*AccountLoadInfo{
				1: {AccountID: 1, CurrentConcurrency: 1, LoadRate: 100},
				2: {AccountID: 2, CurrentConcurrency: 1, LoadRate: 100},
			},
			freshLoadMap: map[int64]*AccountLoadInfo{
				1: {AccountID: 1, CurrentConcurrency: 1, LoadRate: 100},
				2: {AccountID: 2, CurrentConcurrency: 0, LoadRate: 0},
			},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, true, nil)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), nil, sessionHash, "", nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.True(t, selection.Acquired)
		require.True(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(2), selection.Account.ID)
		require.Equal(t, int64(1), cache.bindings[sessionHash])
		require.Empty(t, cache.setCalls)
		require.Equal(t, 2, concurrencyCache.loadBatchCalls)
		require.Equal(t, []int64{1, 2}, concurrencyCache.acquireCalls)
		selection.ReleaseFunc()
	})

	t.Run("queue full retry explicitly preserves the original binding", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformCopilot, 1),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{2: true},
			loadMap: map[int64]*AccountLoadInfo{
				2: {AccountID: 2, CurrentConcurrency: 0, LoadRate: 0},
			},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, true, nil)
		ctx := WithPreserveCopilotStickyBinding(context.Background())
		excluded := map[int64]struct{}{1: {}}

		selection, err := service.SelectAccountWithLoadAwareness(ctx, nil, sessionHash, "", excluded, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.True(t, selection.Acquired)
		require.True(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(2), selection.Account.ID)
		require.Equal(t, int64(1), cache.bindings[sessionHash])
		require.Empty(t, cache.setCalls)
		require.Equal(t, []int64{2}, concurrencyCache.acquireCalls)
		selection.ReleaseFunc()
	})

	t.Run("excluded failed sticky without overflow context rebinds normally", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformCopilot, 1),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{2: true},
			loadMap: map[int64]*AccountLoadInfo{
				2: {AccountID: 2, CurrentConcurrency: 0, LoadRate: 0},
			},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, true, nil)
		excluded := map[int64]struct{}{1: {}}

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), nil, sessionHash, "", excluded, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.True(t, selection.Acquired)
		require.False(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(2), selection.Account.ID)
		require.Equal(t, int64(2), cache.bindings[sessionHash])
		require.Equal(t, []int64{2}, cache.setCalls)
		require.Equal(t, []int64{2}, concurrencyCache.acquireCalls)
		selection.ReleaseFunc()
	})

	t.Run("legacy overflow scans a busy mixed-platform account", func(t *testing.T) {
		accounts := []Account{
			copilotStickyTestAccount(1, PlatformCopilot, 1),
			copilotStickyTestAccount(2, PlatformAnthropic, 2),
			copilotStickyTestAccount(3, PlatformCopilot, 3),
		}
		cache := &copilotStickyTestGatewayCache{bindings: map[string]int64{sessionHash: 1}}
		concurrencyCache := &copilotStickyTestConcurrencyCache{
			acquireResults: map[int64]bool{1: false, 2: false, 3: true},
		}
		service := newCopilotStickyTestService(accounts, cache, concurrencyCache, false, nil)

		selection, err := service.SelectAccountWithLoadAwareness(context.Background(), nil, sessionHash, "", nil, "", 0)
		require.NoError(t, err)
		require.NotNil(t, selection)
		require.True(t, selection.Acquired)
		require.True(t, selection.PreserveStickyBinding)
		require.Equal(t, int64(3), selection.Account.ID)
		require.Equal(t, int64(1), cache.bindings[sessionHash])
		require.Empty(t, cache.setCalls)
		require.Equal(t, []int64{1, 2, 3}, concurrencyCache.acquireCalls)
		selection.ReleaseFunc()
	})
}
