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

function makeCopilotBillingUsage(grossQuantity: number) {
  return {
    username: 'octocat',
    period: '2026-03',
    items_count: 1,
    gross_quantity: grossQuantity,
    gross_amount: grossQuantity / 100,
    net_quantity: grossQuantity,
    net_amount: grossQuantity / 100,
    fetched_at: '2026-03-15T00:00:00Z',
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

    expect(wrapper.text()).toContain('4,000.00/5,000.00')
  })

  it('shows Copilot billing credits without lossy one-decimal k rounding', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          copilot_billing_usage: {
            username: 'octocat',
            period: '2026-03',
            items_count: 1,
            gross_quantity: 19949.375,
            gross_amount: 199.49,
            net_quantity: 19949.375,
            net_amount: 199.49,
            fetched_at: '2026-03-15T00:00:00Z',
          },
        }),
      },
    })

    expect(wrapper.text()).toContain('19,949.375/20,000.00')
    expect(wrapper.get('[title*="gross 19,949.375 / 20,000.00 AI credits"]')).toBeTruthy()
  })

  it('uses the default 200-credit prevention margin', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          copilot_billing_usage: makeCopilotBillingUsage(19800),
        }),
      },
    })

    const billingBadge = wrapper.get('[title^="GitHub Billing"]')
    expect(billingBadge.classes()).toContain('bg-red-100')
    expect(billingBadge.attributes('title')).toContain('prevention threshold 19,800.00')
  })

  it('honors zero and custom Copilot prevention margins', () => {
    const zeroMarginWrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          extra: { billing_safety_margin: 0 },
          copilot_billing_usage: makeCopilotBillingUsage(19999),
        }),
      },
    })
    const zeroMarginBadge = zeroMarginWrapper.get('[title^="GitHub Billing"]')
    expect(zeroMarginBadge.classes()).not.toContain('bg-red-100')
    expect(zeroMarginBadge.attributes('title')).toContain('prevention threshold 20,000.00')

    const customMarginWrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          extra: { billing_safety_margin: 500 },
          copilot_billing_usage: makeCopilotBillingUsage(19500),
        }),
      },
    })
    const customMarginBadge = customMarginWrapper.get('[title^="GitHub Billing"]')
    expect(customMarginBadge.classes()).toContain('bg-red-100')
    expect(customMarginBadge.attributes('title')).toContain('prevention threshold 19,500.00')
  })

  it('does not apply the prevention threshold when auto-pause is disabled', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          extra: { billing_auto_pause_disabled: true },
          copilot_billing_usage: makeCopilotBillingUsage(19900),
        }),
      },
    })
    const billingBadge = wrapper.get('[title^="GitHub Billing"]')
    expect(billingBadge.classes()).not.toContain('bg-red-100')
    expect(billingBadge.attributes('title')).toContain('prevention threshold 19,800.00 (disabled)')

    const pausedWrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          extra: { billing_auto_pause_disabled: true },
          copilot_billing_usage: makeCopilotBillingUsage(19900),
          temp_unschedulable_reason: 'copilot_monthly_quota_exceeded',
          temp_unschedulable_until: '2099-08-01T03:15:00Z',
        }),
      },
    })
    expect(pausedWrapper.get('[title^="GitHub Billing"]').classes()).toContain('bg-red-100')
    expect(pausedWrapper.get('[data-testid="copilot-monthly-quota-pause"]').classes()).toContain('bg-red-100')
  })

  it('shows the Copilot monthly quota pause-until date', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          temp_unschedulable_reason: 'copilot_monthly_quota_exceeded',
          temp_unschedulable_until: '2099-08-01T03:15:00Z',
        }),
      },
    })

    const pause = wrapper.get('[data-testid="copilot-monthly-quota-pause"]')
    expect(pause.text()).toContain('Paused until')
    expect(pause.text()).toContain('2099-08-01 03:15 UTC')
    expect(pause.attributes('title')).toContain('Copilot monthly quota exceeded')
  })

  it('does not show the Copilot pause badge for another temporary reason', () => {
    const wrapper = mount(AccountCapacityCell, {
      props: {
        account: makeAccount({
          temp_unschedulable_reason: 'oauth_401',
          temp_unschedulable_until: '2099-08-01T03:15:00Z',
        }),
      },
    })

    expect(wrapper.find('[data-testid="copilot-monthly-quota-pause"]').exists()).toBe(false)
  })
})
