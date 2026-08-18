import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar hidden version badge', () => {
  it('keeps the application version out of the sidebar header', () => {
    expect(componentSource).not.toContain('<VersionBadge')
    expect(componentSource).not.toContain("import VersionBadge from '@/components/common/VersionBadge.vue'")
    expect(componentSource).not.toContain('const siteVersion = computed(() => appStore.siteVersion)')
  })
})

describe('AppSidebar model plaza navigation', () => {
  it('keeps the model plaza out of the left sidebar', () => {
    expect(componentSource).not.toContain("path: '/models'")
    expect(componentSource).not.toContain("t('nav.modelPlaza')")
  })
})

describe('AppSidebar hidden commercial-code navigation', () => {
  it('keeps redeem-code and promo-code management out of the left sidebar', () => {
    expect(componentSource).not.toContain("path: '/admin/redeem'")
    expect(componentSource).not.toContain("path: '/admin/promo-codes'")
    expect(componentSource).not.toContain("t('nav.redeemCodes')")
    expect(componentSource).not.toContain("t('nav.promoCodes')")
  })
})

describe('AppSidebar hidden channel-management navigation', () => {
  it('keeps channel management and its child links out of the left sidebar', () => {
    expect(componentSource).not.toContain("path: '/admin/channels'")
    expect(componentSource).not.toContain("path: '/admin/channels/pricing'")
    expect(componentSource).not.toContain("path: '/admin/channels/monitor'")
    expect(componentSource).not.toContain("t('nav.channelManagement')")
  })
})
