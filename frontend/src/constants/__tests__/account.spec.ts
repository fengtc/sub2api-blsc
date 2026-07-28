import { describe, expect, it } from 'vitest'
import { buildCopilotBillingPATCreationURL } from '../account'

describe('buildCopilotBillingPATCreationURL', () => {
  it('prefills the minimum personal billing permission and account target', () => {
    const url = new URL(buildCopilotBillingPATCreationURL('octocat'))

    expect(url.origin + url.pathname).toBe(
      'https://github.com/settings/personal-access-tokens/new'
    )
    expect(url.searchParams.get('name')).toBe('Sub2API Billing')
    expect(url.searchParams.get('expires_in')).toBe('90')
    expect(url.searchParams.get('plan')).toBe('read')
    expect(url.searchParams.get('target_name')).toBe('octocat')
  })
})
