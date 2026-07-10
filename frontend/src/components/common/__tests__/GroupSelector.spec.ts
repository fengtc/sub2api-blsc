import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import GroupSelector from '@/components/common/GroupSelector.vue'
import type { AdminGroup, GroupPlatform } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params && typeof params.count !== 'undefined' ? `${key}:${params.count}` : key
    })
  }
})

function group(id: number, name: string, platform: GroupPlatform): AdminGroup {
  return {
    id,
    name,
    description: null,
    platform,
    rate_multiplier: 1,
    is_exclusive: false,
    status: 'active',
    subscription_type: 'shared',
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    allow_image_generation: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    claude_code_only: false,
    fallback_group_id: null,
    fallback_group_id_on_invalid_request: null,
    require_oauth_only: false,
    require_privacy_set: false,
    created_at: '',
    updated_at: '',
    model_routing: null,
    model_routing_enabled: false,
    mcp_xml_inject: false,
    account_count: 0
  } as AdminGroup
}

const groups = [
  group(1, 'claude', 'anthropic'),
  group(2, 'openai', 'openai'),
  group(3, 'gemini', 'gemini'),
  group(4, 'antigravity', 'antigravity'),
  group(5, 'copilot', 'copilot')
]

describe('GroupSelector', () => {
  it('allows copilot accounts to select copilot, anthropic, and openai groups', () => {
    const wrapper = mount(GroupSelector, {
      props: {
        modelValue: [],
        groups,
        platform: 'copilot',
        searchable: false
      },
      global: {
        stubs: {
          Icon: true,
          GroupBadge: {
            props: ['name'],
            template: '<span>{{ name }}</span>'
          }
        }
      }
    })

    expect(wrapper.text()).toContain('claude')
    expect(wrapper.text()).toContain('openai')
    expect(wrapper.text()).toContain('copilot')
    expect(wrapper.text()).not.toContain('gemini')
    expect(wrapper.text()).not.toContain('antigravity')
  })
})
