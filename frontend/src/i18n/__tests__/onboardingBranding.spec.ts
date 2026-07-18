import { describe, expect, it } from 'vitest'

import enMisc from '../locales/en/misc'
import zhMisc from '../locales/zh/misc'

const legacyBrand = /Sub2API/

describe('onboarding branding', () => {
  it('uses the Para AICoding Gateway brand in Chinese onboarding', () => {
    expect(zhMisc.onboarding.admin.welcome.title).toContain('Para AICoding Gateway')
    expect(zhMisc.onboarding.user.welcome.title).toContain('Para AICoding Gateway')
    expect(JSON.stringify(zhMisc.onboarding)).not.toMatch(legacyBrand)
  })

  it('uses the Para AICoding Gateway brand in English onboarding', () => {
    expect(enMisc.onboarding.admin.welcome.title).toContain('Para AICoding Gateway')
    expect(enMisc.onboarding.user.welcome.title).toContain('Para AICoding Gateway')
    expect(JSON.stringify(enMisc.onboarding)).not.toMatch(legacyBrand)
  })
})
