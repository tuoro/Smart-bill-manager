<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { ApiError, api, type ChatConnector } from '../../data/client'

// 与 AI 配置同一套：保存 → 检测 → 启用。密钥只上送一次，之后页面只知道「已设置」。
const connector = ref<ChatConnector>()
const loading = ref(true)
const error = ref('')
const busy = ref('')
const appKey = ref('')
const appSecret = ref('')
let live = true
onBeforeUnmount(() => {
  live = false
  appSecret.value = ''
})

const detectionLabels: Record<ChatConnector['detection_status'], string> = {
  pending: '待检测',
  passed: '检测通过',
  failed: '检测失败',
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    connector.value = await api.chatConnector('dingtalk')
    if (live) appKey.value = connector.value.app_key
  } catch (caught) {
    if (!live) return
    if (caught instanceof ApiError && caught.status === 404) connector.value = undefined
    else error.value = caught instanceof ApiError ? caught.message : '加载失败'
  } finally {
    if (live) loading.value = false
  }
}
onMounted(load)

async function run(name: string, action: () => Promise<ChatConnector>) {
  if (busy.value) return
  busy.value = name
  error.value = ''
  try {
    const updated = await action()
    if (!live) return
    connector.value = updated
    appKey.value = updated.app_key
  } catch (caught) {
    if (live) error.value = caught instanceof ApiError ? caught.message : `${name}失败`
  } finally {
    if (live) busy.value = ''
  }
}

async function save() {
  if (!appKey.value.trim() || !appSecret.value) {
    error.value = '请填写 AppKey 与 AppSecret'
    return
  }
  const secret = appSecret.value
  appSecret.value = ''
  await run('保存', () => api.saveChatConnector('dingtalk', appKey.value.trim(), secret))
}

function formatMoment(value: string | null | undefined) {
  if (!value) return ''
  const instant = new Date(value)
  if (Number.isNaN(instant.getTime())) return ''
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(
    instant,
  )
}
</script>

<template>
  <section class="page-stack settings-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <span>系统</span><span aria-hidden="true">/</span><strong>钉钉收单</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>钉钉收单</h1>
        <p>把单据直接发给钉钉机器人即可进入识别队列，不必先打开网页。</p>
      </div>
    </header>
    <div class="provider-overview">
      <ol class="provider-step-guide" aria-label="配置使用步骤">
        <li>
          <span aria-hidden="true">1</span>
          <div><strong>保存凭据</strong><small>钉钉开放平台应用的 AppKey / AppSecret</small></div>
        </li>
        <li>
          <span aria-hidden="true">2</span>
          <div><strong>检测</strong><small>向钉钉换一次令牌，验证凭据有效</small></div>
        </li>
        <li>
          <span aria-hidden="true">3</span>
          <div><strong>启用</strong><small>建立长连接，成员即可绑定并投递</small></div>
        </li>
      </ol>
    </div>
    <div v-if="error" class="notice notice-danger" role="alert">
      <span aria-hidden="true">!</span><span>{{ error }}</span>
    </div>

    <div v-if="loading" class="panel state-layout" role="status">
      <span class="spinner spinner-large" aria-hidden="true"></span><strong>正在读取配置</strong>
    </div>
    <div v-else class="settings-grid">
      <section class="panel" aria-labelledby="connector-status-title">
        <div class="panel-heading">
          <div>
            <h2 id="connector-status-title">当前状态</h2>
            <p>凭据按当前工作区保存，同一个 AppKey 不能被两个工作区使用。</p>
          </div>
        </div>
        <div v-if="!connector" class="state-layout compact">
          <strong>尚未配置</strong>
          <span>在右侧填入凭据并保存。</span>
        </div>
        <dl v-else class="connector-status">
          <div>
            <dt>AppKey</dt>
            <dd>
              <code>{{ connector.app_key }}</code>
            </dd>
          </div>
          <div>
            <dt>AppSecret</dt>
            <dd>{{ connector.has_secret ? '已设置（不回显）' : '未设置' }}</dd>
          </div>
          <div>
            <dt>检测</dt>
            <dd>
              <span
                class="status"
                :data-tone="
                  connector.detection_status === 'passed'
                    ? 'success'
                    : connector.detection_status === 'failed'
                      ? 'danger'
                      : 'warning'
                "
                ><span aria-hidden="true">●</span
                >{{ detectionLabels[connector.detection_status] }}</span
              >
              <small v-if="connector.detection_message">
                {{ connector.detection_message }}
                <template v-if="connector.detection_checked_at">
                  · {{ formatMoment(connector.detection_checked_at) }}</template
                >
              </small>
            </dd>
          </div>
          <div>
            <dt>连接</dt>
            <dd>
              <span class="status" :data-tone="connector.active ? 'success' : 'neutral'"
                ><span aria-hidden="true">●</span>{{ connector.active ? '已启用' : '未启用' }}</span
              >
            </dd>
          </div>
        </dl>
        <div v-if="connector" class="connector-actions">
          <button
            class="button"
            type="button"
            :disabled="Boolean(busy)"
            @click="run('检测', () => api.detectChatConnector('dingtalk'))"
          >
            {{ busy === '检测' ? '检测中…' : '检测凭据' }}
          </button>
          <button
            v-if="!connector.active"
            class="button button-primary"
            type="button"
            :disabled="Boolean(busy) || connector.detection_status !== 'passed'"
            @click="run('启用', () => api.activateChatConnector('dingtalk'))"
          >
            启用
          </button>
          <button
            v-else
            class="button"
            type="button"
            :disabled="Boolean(busy)"
            @click="run('停用', () => api.deactivateChatConnector('dingtalk'))"
          >
            停用
          </button>
        </div>
        <p v-if="connector?.active" class="form-note connector-note">
          已连接。成员到
          <RouterLink to="/settings/account">账号与密码</RouterLink>
          生成绑定码发给机器人即可开始投递。
        </p>
      </section>

      <section class="panel provider-form-panel" aria-labelledby="connector-form-title">
        <div class="panel-heading">
          <div>
            <h2 id="connector-form-title">{{ connector ? '更换凭据' : '填写凭据' }}</h2>
            <p>钉钉开放平台 → 企业内部应用 → 凭证与基础信息。</p>
          </div>
        </div>
        <form class="stack-form" @submit.prevent="save">
          <label class="field-stack"
            ><span>AppKey</span
            ><input
              v-model.trim="appKey"
              class="input"
              type="text"
              maxlength="200"
              autocomplete="off"
              required /></label
          ><label class="field-stack"
            ><span>AppSecret</span
            ><input
              v-model="appSecret"
              class="input"
              type="password"
              maxlength="512"
              autocomplete="off"
              required
              aria-describedby="connector-secret-note"
          /></label>
          <p id="connector-secret-note" class="form-note">
            加密保存，提交后不回显。更换凭据会重置检测并断开当前连接，需要重新检测、启用。
          </p>
          <button
            class="button button-primary button-block"
            type="submit"
            :disabled="Boolean(busy)"
          >
            {{ busy === '保存' ? '正在保存…' : connector ? '保存新凭据' : '保存凭据' }}
          </button>
        </form>
      </section>
    </div>
  </section>
</template>
