<template>
  <div class="space-y-2">
    <button
      type="button"
      class="btn btn-secondary"
      :disabled="loading || polling"
      data-testid="copilot-device-authorize"
      @click="startAuthorization"
    >
      {{ loading ? '正在获取验证码…' : polling ? '等待 GitHub 授权…' : 'GitHub 授权获取' }}
    </button>

    <div
      v-if="userCode"
      class="space-y-2 rounded-lg border border-blue-200 bg-blue-50 p-3 dark:border-blue-900 dark:bg-blue-950/30"
      data-testid="copilot-device-authorization"
    >
      <p class="text-sm text-gray-700 dark:text-gray-300">
        在 GitHub 授权页面输入以下验证码：
      </p>
      <div class="flex flex-wrap items-center gap-2">
        <code class="rounded bg-white px-3 py-2 text-lg font-semibold tracking-widest text-gray-900 dark:bg-dark-700 dark:text-white">
          {{ userCode }}
        </code>
        <button type="button" class="btn btn-secondary btn-sm" @click="copyUserCode">
          复制验证码
        </button>
        <a
          :href="verificationURI"
          target="_blank"
          rel="noopener noreferrer"
          class="btn btn-primary btn-sm"
        >
          打开 GitHub 授权页面
        </a>
      </div>
      <p class="text-xs text-gray-600 dark:text-gray-400">{{ statusMessage }}</p>
      <button type="button" class="text-xs text-gray-500 underline" @click="cancelAuthorization">
        取消授权
      </button>
    </div>

    <p v-if="errorMessage" class="text-xs text-red-600 dark:text-red-400">
      {{ errorMessage }}
    </p>
    <p v-else-if="successMessage" class="text-xs text-green-600 dark:text-green-400">
      {{ successMessage }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue'
import { adminAPI } from '@/api/admin'

const emit = defineEmits<{
  authorized: [payload: { token: string; username: string }]
}>()

const loading = ref(false)
const polling = ref(false)
const userCode = ref('')
const verificationURI = ref('')
const statusMessage = ref('')
const errorMessage = ref('')
const successMessage = ref('')

let flowID = ''
let pollTimer: ReturnType<typeof setTimeout> | null = null
let flowGeneration = 0

const clearPollTimer = () => {
  if (pollTimer !== null) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
}

const resetActiveFlow = () => {
  clearPollTimer()
  flowID = ''
  polling.value = false
  userCode.value = ''
  verificationURI.value = ''
  statusMessage.value = ''
}

const failAuthorization = (error: unknown) => {
  const candidate = error as {
    response?: { data?: { message?: string; detail?: string } }
    message?: string
  }
  errorMessage.value =
    candidate.response?.data?.message ||
    candidate.response?.data?.detail ||
    candidate.message ||
    'GitHub 授权失败'
  resetActiveFlow()
}

const schedulePoll = (delaySeconds: number, generation: number) => {
  clearPollTimer()
  pollTimer = setTimeout(() => {
    void pollAuthorization(generation)
  }, Math.max(delaySeconds, 1) * 1000)
}

const pollAuthorization = async (generation: number) => {
  if (!flowID || generation !== flowGeneration) return
  try {
    const result = await adminAPI.accounts.pollCopilotDeviceAuthorization(flowID)
    if (generation !== flowGeneration) return
    if (result.status === 'authorized' && result.access_token) {
      const token = result.access_token
      const username = result.username || ''
      resetActiveFlow()
      successMessage.value = 'GitHub 授权成功，Token 已自动填入。'
      emit('authorized', { token, username })
      return
    }
    statusMessage.value = '等待你在 GitHub 完成授权…'
    schedulePoll(result.retry_after || 5, generation)
  } catch (error) {
    if (generation === flowGeneration) failAuthorization(error)
  }
}

const startAuthorization = async () => {
  flowGeneration += 1
  const generation = flowGeneration
  resetActiveFlow()
  loading.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const result = await adminAPI.accounts.startCopilotDeviceAuthorization()
    if (generation !== flowGeneration) return
    flowID = result.flow_id
    userCode.value = result.user_code
    verificationURI.value = result.verification_uri
    statusMessage.value = '请打开 GitHub 页面并输入验证码。授权后本页面会自动完成。'
    polling.value = true
    schedulePoll(result.interval || 5, generation)
  } catch (error) {
    if (generation === flowGeneration) failAuthorization(error)
  } finally {
    if (generation === flowGeneration) loading.value = false
  }
}

const copyUserCode = async () => {
  if (!userCode.value) return
  try {
    await navigator.clipboard.writeText(userCode.value)
    statusMessage.value = '验证码已复制；请在 GitHub 授权页面粘贴。'
  } catch {
    statusMessage.value = '无法自动复制，请手动复制验证码。'
  }
}

const cancelAuthorization = () => {
  flowGeneration += 1
  resetActiveFlow()
  errorMessage.value = ''
}

onBeforeUnmount(cancelAuthorization)
</script>
