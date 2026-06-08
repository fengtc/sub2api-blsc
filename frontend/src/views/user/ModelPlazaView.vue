<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div class="flex flex-1 flex-col gap-3 sm:flex-row sm:items-center">
            <div class="relative w-full sm:max-w-sm">
              <Icon
                name="search"
                size="md"
                class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="text"
                :placeholder="t('modelPlaza.searchPlaceholder')"
                class="input pl-10"
              />
            </div>

            <select v-model="providerFilter" class="input w-full sm:w-48">
              <option value="">{{ t('modelPlaza.allProviders') }}</option>
              <option v-for="provider in providers" :key="provider" :value="provider">
                {{ provider }}
              </option>
            </select>
          </div>

          <div class="text-sm text-gray-500 dark:text-dark-400">
            {{ t('modelPlaza.modelCount', { count: filteredModels.length }) }}
          </div>
        </div>
      </template>

      <template #table>
        <div class="table-wrapper">
          <table>
            <thead>
              <tr>
                <th>{{ t('modelPlaza.columns.model') }}</th>
                <th>{{ t('modelPlaza.columns.provider') }}</th>
                <th>{{ t('modelPlaza.columns.endpoint') }}</th>
                <th>{{ t('modelPlaza.columns.upstream') }}</th>
                <th class="text-right">{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="model in filteredModels" :key="model.name">
                <td>
                  <code class="model-code">{{ model.name }}</code>
                </td>
                <td>
                  <span class="provider-badge">{{ model.provider }}</span>
                </td>
                <td>
                  <code class="endpoint-code">{{ model.endpoint }}</code>
                </td>
                <td>
                  <code class="upstream-code">{{ model.upstream }}</code>
                </td>
                <td>
                  <div class="flex justify-end">
                    <button
                      type="button"
                      class="btn btn-secondary btn-sm"
                      :title="t('modelPlaza.copyModel')"
                      @click="copyModel(model.name)"
                    >
                      <Icon name="copy" size="sm" />
                      <span>{{ t('common.copy') }}</span>
                    </button>
                  </div>
                </td>
              </tr>
              <tr v-if="filteredModels.length === 0">
                <td colspan="5" class="py-12 text-center text-gray-500 dark:text-dark-400">
                  {{ t('modelPlaza.empty') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'

type ModelEntry = {
  name: string
  provider: string
  endpoint: '/v1/chat/completions' | '/v1/messages'
  upstream: string
}

const { t } = useI18n()
const { copyToClipboard } = useClipboard()

const models: ModelEntry[] = [
  { name: 'claude-opus-4-8', provider: 'Claude', endpoint: '/v1/messages', upstream: 'anthropic/claude-opus-4.8' },
  { name: 'claude-opus-4-7', provider: 'Claude', endpoint: '/v1/messages', upstream: 'anthropic/claude-opus-4.7' },
  { name: 'claude-sonnet-4-6', provider: 'Claude', endpoint: '/v1/messages', upstream: 'anthropic/claude-sonnet-4.6' },
  { name: 'claude-opus-4-6', provider: 'Claude', endpoint: '/v1/messages', upstream: 'anthropic/claude-opus-4.6' },
  { name: 'claude-haiku-4-5-20251001', provider: 'Claude', endpoint: '/v1/messages', upstream: 'anthropic/claude-haiku-4.5' },
  { name: 'gpt-5.3-codex', provider: 'OpenAI', endpoint: '/v1/chat/completions', upstream: 'openai/gpt-5.3-codex' },
  { name: 'gpt-5.5', provider: 'OpenAI', endpoint: '/v1/chat/completions', upstream: 'openai/gpt-5.5' },
  { name: 'deepseek-r1', provider: 'DeepSeek', endpoint: '/v1/chat/completions', upstream: 'deepseek/deepseek-r1' },
  { name: 'deepseek-v4-flash', provider: 'DeepSeek', endpoint: '/v1/chat/completions', upstream: 'deepseek/deepseek-v4-flash' },
  { name: 'deepseek-v4-pro', provider: 'DeepSeek', endpoint: '/v1/chat/completions', upstream: 'deepseek/deepseek-v4-pro' },
  { name: 'gemini-3.5-flash', provider: 'Google', endpoint: '/v1/chat/completions', upstream: 'google/gemini-3.5-flash' },
  { name: 'glm-5.1', provider: '智谱', endpoint: '/v1/chat/completions', upstream: 'z-ai/glm-5.1' },
  { name: 'gpt-oss-120b:free', provider: 'OpenAI', endpoint: '/v1/chat/completions', upstream: 'openai/gpt-oss-120b:free' },
  { name: 'gpt-oss-20b:free', provider: 'OpenAI', endpoint: '/v1/chat/completions', upstream: 'openai/gpt-oss-20b:free' },
  { name: 'grok-4.3', provider: 'xAI', endpoint: '/v1/chat/completions', upstream: 'x-ai/grok-4.3' },
  { name: 'kimi-k2.6', provider: 'Moonshot AI', endpoint: '/v1/chat/completions', upstream: 'moonshotai/kimi-k2.6' },
  { name: 'kimi-k2.6:free', provider: 'Moonshot AI', endpoint: '/v1/chat/completions', upstream: 'moonshotai/kimi-k2.6:free' },
  { name: 'mimo-v2.5', provider: 'Xiaomi', endpoint: '/v1/chat/completions', upstream: 'xiaomi/mimo-v2.5' },
  { name: 'mimo-v2.5-pro', provider: 'Xiaomi', endpoint: '/v1/chat/completions', upstream: 'xiaomi/mimo-v2.5-pro' },
  { name: 'minimax-m3', provider: 'MiniMax', endpoint: '/v1/chat/completions', upstream: 'minimax/minimax-m3' },
  { name: 'nemotron-3-ultra-550b-a55b:free', provider: 'NVIDIA', endpoint: '/v1/chat/completions', upstream: 'nvidia/nemotron-3-ultra-550b-a55b:free' },
  { name: 'phi-4', provider: 'Microsoft', endpoint: '/v1/chat/completions', upstream: 'microsoft/phi-4' },
  { name: 'qwen3.7-max', provider: 'Qwen', endpoint: '/v1/chat/completions', upstream: 'qwen/qwen3.7-max' },
  { name: 'qwen3.7-plus', provider: 'Qwen', endpoint: '/v1/chat/completions', upstream: 'qwen/qwen3.7-plus' },
]

const searchQuery = ref('')
const providerFilter = ref('')

const providers = computed(() => Array.from(new Set(models.map((model) => model.provider))).sort())

const filteredModels = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  return models.filter((model) => {
    const matchesProvider = !providerFilter.value || model.provider === providerFilter.value
    const matchesSearch =
      !q ||
      model.name.toLowerCase().includes(q) ||
      model.provider.toLowerCase().includes(q) ||
      model.upstream.toLowerCase().includes(q)
    return matchesProvider && matchesSearch
  })
})

async function copyModel(modelName: string): Promise<void> {
  await copyToClipboard(modelName, t('modelPlaza.copied'))
}
</script>

<style scoped>
.model-code {
  @apply rounded-md bg-gray-100 px-2 py-1 font-mono text-sm font-semibold text-gray-900 dark:bg-dark-700 dark:text-white;
}

.endpoint-code,
.upstream-code {
  @apply rounded-md bg-gray-50 px-2 py-1 font-mono text-xs text-gray-600 dark:bg-dark-900 dark:text-dark-300;
}

.provider-badge {
  @apply inline-flex items-center rounded-full border border-gray-200 px-2.5 py-1 text-xs font-medium text-gray-700 dark:border-dark-600 dark:text-dark-200;
}
</style>
