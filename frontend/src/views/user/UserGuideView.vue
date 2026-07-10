<template>
  <AppLayout>
    <div class="mx-auto flex w-full max-w-5xl flex-col gap-6">
      <section class="doc-panel">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h2 class="text-xl font-semibold text-gray-900 dark:text-white">
              OIDC 登录与 API Key 连通验证
            </h2>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">
              使用并行账号登录后，分别为 openai 组和 claude 组创建 API Key，再用对应接口验证模型连通。
            </p>
          </div>
          <div class="rounded-lg border border-gray-200 px-3 py-2 text-sm text-gray-600 dark:border-dark-700 dark:text-dark-300">
            https://api.blsc.dev
          </div>
        </div>
      </section>

      <section class="doc-panel">
        <h3 class="doc-title">快速流程</h3>
        <div class="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <div v-for="step in quickSteps" :key="step.title" class="step-box">
            <div class="step-number">{{ step.number }}</div>
            <div>
              <div class="font-medium text-gray-900 dark:text-white">{{ step.title }}</div>
              <p class="mt-1 text-sm leading-5 text-gray-600 dark:text-dark-300">{{ step.text }}</p>
            </div>
          </div>
        </div>
      </section>

      <section class="doc-panel">
        <h3 class="doc-title">创建 OpenAI 组 API Key</h3>
        <ol class="doc-list">
          <li>左侧菜单进入 <strong>模型广场</strong>，复制模型名 <code>gpt-5.5</code>。</li>
          <li>进入 <strong>API 密钥</strong>，点击创建 API Key。</li>
          <li>分组选择 <code>openai</code>，名称可填写 <code>openai-test-key</code>。</li>
          <li>保存后复制生成的 API Key。</li>
        </ol>
        <CodeBlock title="OpenAI 兼容接口连通验证" :code="openAICurl" />
      </section>

      <section class="doc-panel">
        <h3 class="doc-title">创建 Claude 组 API Key</h3>
        <ol class="doc-list">
          <li>左侧菜单进入 <strong>模型广场</strong>，复制模型名 <code>claude-opus-4-8</code>。</li>
          <li>进入 <strong>API 密钥</strong>，点击创建 API Key。</li>
          <li>分组选择 <code>claude</code>，名称可填写 <code>claude-test-key</code>。</li>
          <li>保存后复制生成的 API Key。</li>
        </ol>
        <CodeBlock title="Claude Messages 接口连通验证" :code="claudeCurl" />
      </section>

      <section class="doc-panel">
        <h3 class="doc-title">Postman 配置</h3>
        <div class="grid gap-4 lg:grid-cols-2">
          <div class="info-box">
            <h4 class="info-title">OpenAI 兼容接口</h4>
            <ul class="doc-list mt-3">
              <li>Method: <code>POST</code></li>
              <li>URL: <code>https://api.blsc.dev/v1/chat/completions</code></li>
              <li>Header: <code>Content-Type: application/json</code></li>
              <li>Header: <code>Authorization: Bearer YOUR_OPENAI_API_KEY</code></li>
              <li>Body: <code>raw</code> + <code>JSON</code></li>
            </ul>
          </div>
          <div class="info-box">
            <h4 class="info-title">Claude Messages 接口</h4>
            <ul class="doc-list mt-3">
              <li>Method: <code>POST</code></li>
              <li>URL: <code>https://api.blsc.dev/v1/messages</code></li>
              <li>Header: <code>Content-Type: application/json</code></li>
              <li>Header: <code>x-api-key: YOUR_CLAUDE_API_KEY</code></li>
              <li>Header: <code>anthropic-version: 2023-06-01</code></li>
              <li>Body: <code>raw</code> + <code>JSON</code></li>
            </ul>
          </div>
        </div>
      </section>

      <section class="doc-panel">
        <h3 class="doc-title">判断是否成功</h3>
        <div class="grid gap-4 lg:grid-cols-2">
          <div class="info-box">
            <h4 class="info-title">OpenAI</h4>
            <ul class="doc-list mt-3">
              <li>HTTP 状态码为 <code>200</code>。</li>
              <li>返回里有 <code>choices</code>。</li>
              <li><code>choices[0].message.content</code> 有回复内容。</li>
            </ul>
          </div>
          <div class="info-box">
            <h4 class="info-title">Claude</h4>
            <ul class="doc-list mt-3">
              <li>HTTP 状态码为 <code>200</code>。</li>
              <li>返回里有 <code>content</code>。</li>
              <li><code>content[0].text</code> 有回复内容。</li>
            </ul>
          </div>
        </div>
      </section>

      <section class="doc-panel">
        <h3 class="doc-title">常见问题</h3>
        <div class="divide-y divide-gray-100 dark:divide-dark-700">
          <div v-for="item in faqItems" :key="item.title" class="py-4 first:pt-0 last:pb-0">
            <h4 class="font-medium text-gray-900 dark:text-white">{{ item.title }}</h4>
            <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ item.text }}</p>
          </div>
        </div>
      </section>

      <section class="doc-panel">
        <h3 class="doc-title">安全注意事项</h3>
        <ul class="doc-list">
          <li>不要把 API Key 发到群聊、截图或公开文档里。</li>
          <li>如果 API Key 泄露，立即在 API 密钥页面删除并重新创建。</li>
          <li>OpenAI 组和 Claude 组建议分别创建不同 Key，方便查看用量和排查问题。</li>
          <li>测试完成后，可以在使用记录页面查看输入输出 Token 和费用记录。</li>
        </ul>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { h } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useClipboard } from '@/composables/useClipboard'

const { copyToClipboard } = useClipboard()

const openAICurl = `curl -sS https://api.blsc.dev/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer YOUR_OPENAI_API_KEY" \\
  -d '{
    "model": "gpt-5.5",
    "messages": [
      {
        "role": "user",
        "content": "你好"
      }
    ]
  }'`

const claudeCurl = `curl -sS https://api.blsc.dev/v1/messages \\
  -H "Content-Type: application/json" \\
  -H "x-api-key: YOUR_CLAUDE_API_KEY" \\
  -H "anthropic-version: 2023-06-01" \\
  -d '{
    "model": "claude-opus-4-8",
    "max_tokens": 200,
    "messages": [
      {
        "role": "user",
        "content": "你好"
      }
    ]
  }'`

const quickSteps = [
  { number: '1', title: 'OIDC 登录', text: '使用并行账号完成控制台登录。' },
  { number: '2', title: '复制模型名', text: '在模型广场复制 gpt-5.5 或 claude-opus-4-8。' },
  { number: '3', title: '创建 Key', text: '在 API 密钥页面分别选择 openai 或 claude 分组。' },
  { number: '4', title: 'curl 验证', text: '使用对应 API Key 调用对应接口。' },
]

const faqItems = [
  {
    title: '401 Unauthorized',
    text: '通常是 API Key 不正确、复制时多了空格，或使用了已删除的 Key。请回到 API 密钥页面重新复制完整 Key。',
  },
  {
    title: '403 Forbidden',
    text: '通常是 API Key 没有访问对应分组，或账号权限不足。OpenAI 测试使用 openai 组 Key，Claude 测试使用 claude 组 Key。',
  },
  {
    title: '模型不存在或模型不可用',
    text: '通常是 model 字段写错，或该分组没有配置这个模型。请到模型广场复制精确模型名。',
  },
  {
    title: '请求 200 但没有看到回复',
    text: '非流式请求看完整 JSON；OpenAI 看 choices[0].message.content，Claude 看 content[0].text。流式测试建议 curl 加 -N。',
  },
]

const CodeBlock = (props: { title: string; code: string }) =>
  h('div', { class: 'code-card' }, [
    h('div', { class: 'code-header' }, [
      h('span', { class: 'code-title' }, props.title),
      h(
        'button',
        {
          type: 'button',
          class: 'btn btn-secondary btn-sm',
          onClick: () => copyToClipboard(props.code),
        },
        [h(Icon, { name: 'copy', size: 'sm' }), h('span', '复制')],
      ),
    ]),
    h('pre', { class: 'code-pre' }, [h('code', props.code)]),
  ])
</script>

<style scoped>
.doc-panel {
  @apply rounded-xl border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-800;
}

.doc-title {
  @apply text-base font-semibold text-gray-900 dark:text-white;
}

.doc-list {
  @apply mt-4 space-y-2 text-sm leading-6 text-gray-700 dark:text-dark-200;
}

.doc-list code {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs text-gray-900 dark:bg-dark-900 dark:text-dark-100;
}

.step-box {
  @apply flex gap-3 rounded-lg border border-gray-200 p-4 dark:border-dark-700;
}

.step-number {
  @apply flex h-7 w-7 flex-shrink-0 items-center justify-center rounded-full bg-primary-600 text-sm font-semibold text-white;
}

.info-box {
  @apply rounded-lg border border-gray-200 p-4 dark:border-dark-700;
}

.info-title {
  @apply text-sm font-semibold text-gray-900 dark:text-white;
}

.code-card {
  @apply mt-5 overflow-hidden rounded-lg border border-gray-200 dark:border-dark-700;
}

.code-header {
  @apply flex items-center justify-between gap-3 border-b border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-900;
}

.code-title {
  @apply text-sm font-medium text-gray-800 dark:text-dark-100;
}

.code-pre {
  @apply overflow-x-auto bg-gray-950 p-4 text-sm leading-6 text-gray-100;
}
</style>
