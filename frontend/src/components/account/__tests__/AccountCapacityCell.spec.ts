import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountCapacityCell from '../AccountCapacityCell.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'account',
    platform: 'copilot',
    type: 'apikey',
    proxy_id: null,
    concurrency: 3,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-03-15T00:00:00Z',
    updated_at: '2026-03-15T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides,
  }
}

describe('AccountCapacityCell', () => {
  it('shows local window cost limit for Copilot accounts', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          current_window_cost: 26.97,
          window_cost_limit: 30,
          window_cost_sticky_reserve: 5,
        }),
      },
    })

    expect(wrapper.text()).toContain('$26.97/$30.00')
  })

  it('uses the configured Copilot billing credit limit', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          extra: { billing_credit_limit: 5000 },
          copilot_billing_usage: {
            username: 'octocat',
            period: '2026-03',
            items_count: 1,
            gross_quantity: 4000,
            gross_amount: 40,
            net_quantity: 4000,
            net_amount: 40,
            fetched_at: '2026-03-15T00:00:00Z',
          },
        }),
      },
    })

    expect(wrapper.text()).toContain('4.00k/5.00k')
  })
})
