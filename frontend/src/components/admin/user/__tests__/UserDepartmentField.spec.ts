import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { AdminUser, UserAttributeDefinition } from '@/types'
import UserAttributeForm from '@/components/user/UserAttributeForm.vue'
import UserCreateModal from '../UserCreateModal.vue'
import UserEditModal from '../UserEditModal.vue'

const {
  createUser,
  updateUser,
  listEnabledDefinitions,
  getUserAttributeValues,
  updateUserAttributeValues,
  showSuccess,
  showError
} = vi.hoisted(() => ({
  createUser: vi.fn(),
  updateUser: vi.fn(),
  listEnabledDefinitions: vi.fn(),
  getUserAttributeValues: vi.fn(),
  updateUserAttributeValues: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      create: createUser,
      update: updateUser
    },
    userAttributes: {
      listEnabledDefinitions,
      getUserAttributeValues,
      updateUserAttributeValues
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const departmentDefinition: UserAttributeDefinition = {
  id: 2,
  key: 'department',
  name: '部门',
  description: '用户所属部门，支持手工录入和批量导入',
  type: 'text',
  options: [],
  required: false,
  validation: {},
  placeholder: '请输入部门名称',
  display_order: 0,
  enabled: true,
  created_at: '2026-08-18T00:00:00Z',
  updated_at: '2026-08-18T00:00:00Z'
}

const dialogStubs = {
  BaseDialog: {
    props: ['show', 'title', 'width'],
    emits: ['close'],
    template: '<div v-if="show"><slot /><slot name="footer" /></div>'
  },
  TotpStepUpDialog: true,
  Icon: true
}

const createAdminUser = (): AdminUser => ({
  id: 88,
  email: 'new@example.com',
  username: 'new-user',
  role: 'user',
  balance: 0,
  concurrency: 1,
  rpm_limit: 0,
  tpm_limit: 0,
  status: 'active',
  allowed_groups: [],
  balance_notify_enabled: false,
  balance_notify_threshold: null,
  balance_notify_extra_emails: [],
  notes: '',
  created_at: '2026-08-18T00:00:00Z',
  updated_at: '2026-08-18T00:00:00Z'
})

beforeEach(() => {
  vi.clearAllMocks()
  listEnabledDefinitions.mockResolvedValue([departmentDefinition])
  getUserAttributeValues.mockResolvedValue([])
  updateUserAttributeValues.mockResolvedValue({ message: 'ok' })
  createUser.mockResolvedValue(createAdminUser())
  updateUser.mockResolvedValue(createAdminUser())
})

describe('department field in admin user forms', () => {
  it('can isolate the department definition from other custom attributes', async () => {
    listEnabledDefinitions.mockResolvedValue([
      departmentDefinition,
      { ...departmentDefinition, id: 3, key: 'employee_code', name: '工号' }
    ])

    const wrapper = mount(UserAttributeForm, {
      props: {
        modelValue: {},
        includeKeys: ['department']
      }
    })
    await flushPromises()

    expect(wrapper.find('[data-attribute-key="department"]').exists()).toBe(true)
    expect(wrapper.find('[data-attribute-key="employee_code"]').exists()).toBe(false)
  })

  it('places department between username and role and saves it after user creation', async () => {
    const wrapper = mount(UserCreateModal, {
      props: { show: true },
      global: { stubs: dialogStubs }
    })
    await flushPromises()

    const html = wrapper.html()
    expect(html.indexOf('admin.users.username')).toBeLessThan(html.indexOf('data-test="user-department-field"'))
    expect(html.indexOf('data-test="user-department-field"')).toBeLessThan(html.indexOf('admin.users.form.roleLabel'))
    expect(wrapper.findAll('[data-attribute-key="department"]')).toHaveLength(1)

    await wrapper.get('input[type="email"]').setValue('new@example.com')
    await wrapper.get('input[required][type="text"]').setValue('StrongPassword123!')
    await wrapper.get('[data-attribute-key="department"] input').setValue('研发部')
    await wrapper.get('#create-user-form').trigger('submit')
    await flushPromises()

    expect(createUser).toHaveBeenCalledWith(expect.not.objectContaining({ customAttributes: expect.anything() }))
    expect(updateUserAttributeValues).toHaveBeenCalledWith(88, { 2: '研发部' })
  })

  it('places department between username and role and loads it for editing', async () => {
    getUserAttributeValues.mockResolvedValue([
      {
        id: 10,
        user_id: 88,
        attribute_id: 2,
        value: '市场部',
        created_at: '2026-08-18T00:00:00Z',
        updated_at: '2026-08-18T00:00:00Z'
      }
    ])

    const wrapper = mount(UserEditModal, {
      props: { show: true, user: createAdminUser() },
      global: { stubs: dialogStubs }
    })
    await flushPromises()

    const html = wrapper.html()
    expect(html.indexOf('admin.users.username')).toBeLessThan(html.indexOf('data-test="user-department-field"'))
    expect(html.indexOf('data-test="user-department-field"')).toBeLessThan(html.indexOf('admin.users.form.roleLabel'))
    expect(wrapper.findAll('[data-attribute-key="department"]')).toHaveLength(1)

    const departmentInput = wrapper.get('[data-attribute-key="department"] input')
    expect((departmentInput.element as HTMLInputElement).value).toBe('市场部')
    await departmentInput.setValue('财务部')
    await wrapper.get('#edit-user-form').trigger('submit')
    await flushPromises()

    expect(updateUser).toHaveBeenCalledWith(88, expect.objectContaining({ username: 'new-user' }))
    expect(updateUserAttributeValues).toHaveBeenCalledWith(88, { 2: '财务部' })
  })
})
