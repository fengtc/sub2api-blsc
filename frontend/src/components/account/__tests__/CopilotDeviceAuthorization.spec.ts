import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CopilotDeviceAuthorization from '../CopilotDeviceAuthorization.vue'

const { startCopilotDeviceAuthorization, pollCopilotDeviceAuthorization } = vi.hoisted(() => ({
  startCopilotDeviceAuthorization: vi.fn(),
  pollCopilotDeviceAuthorization: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      startCopilotDeviceAuthorization,
      pollCopilotDeviceAuthorization
    }
  }
}))

describe('CopilotDeviceAuthorization', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    startCopilotDeviceAuthorization.mockResolvedValue({
      flow_id: 'flow-id',
      user_code: 'ABCD-1234',
      verification_uri: 'https://github.com/login/device',
      expires_in: 900,
      interval: 5
    })
  })

  it('shows the user code and emits the token after authorization', async () => {
    pollCopilotDeviceAuthorization.mockResolvedValue({
      status: 'authorized',
      access_token: 'gho_secret',
      username: 'octocat'
    })
    const wrapper = mount(CopilotDeviceAuthorization)

    await wrapper.get('[data-testid="copilot-device-authorize"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('ABCD-1234')
    expect(wrapper.get('a').attributes('href')).toBe('https://github.com/login/device')

    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()

    expect(wrapper.emitted('authorized')).toEqual([
      [{ token: 'gho_secret', username: 'octocat' }]
    ])
    expect(wrapper.text()).not.toContain('gho_secret')
    expect(wrapper.text()).toContain('GitHub 授权成功')
  })
})
