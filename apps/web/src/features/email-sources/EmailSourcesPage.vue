<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { sessionStore } from '../../app/session'
import {
  ApiError,
  api,
  type EmailMessage,
  type EmailSource,
  type EmailSourceRegistration,
} from '../../data/client'
import {
  attachmentReasonLabel,
  emailAttachmentStatusMeta,
  emailMessageStatusMeta,
  emailSourceStatusMeta,
  formatArchiveBytes,
} from './model'

const session = sessionStore.current
const canRead = computed(() => session.value?.capabilities.includes('email_archive.read') ?? false)
const canManage = computed(
  () => session.value?.capabilities.includes('email_sources.manage') ?? false,
)
const sources = ref<EmailSource[]>([])
const selectedSourceID = ref('')
const messages = ref<EmailMessage[]>([])
const nextCursor = ref('')
const loading = ref(true)
const messagesLoading = ref(false)
const loadingMore = ref(false)
const creating = ref(false)
const error = ref('')
const offline = ref(!navigator.onLine)
const showRegistration = ref(false)
const displayName = ref('')
const mailboxAddress = ref('')
const imapHost = ref('')
const imapPort = ref(993)
const transportSecurity = ref<EmailSourceRegistration['transport_security']>('implicit_tls')
const registrationKey = ref('')

const selectedSource = computed(
  () => sources.value.find((source) => source.id === selectedSourceID.value) ?? null,
)

watch([displayName, mailboxAddress, imapHost, imapPort, transportSecurity], () => {
  registrationKey.value = ''
})

async function loadSources(preferredSourceID = '') {
  if (!canRead.value || offline.value) {
    loading.value = false
    return
  }
  loading.value = true
  error.value = ''
  try {
    sources.value = (await api.emailSources()).items
    const preferred = preferredSourceID || selectedSourceID.value
    selectedSourceID.value = sources.value.some((source) => source.id === preferred)
      ? preferred
      : (sources.value[0]?.id ?? '')
    messages.value = []
    nextCursor.value = ''
    if (selectedSourceID.value) await loadMessages(false)
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '邮箱来源加载失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

async function selectSource(sourceID: string) {
  if (sourceID === selectedSourceID.value && !error.value) return
  selectedSourceID.value = sourceID
  messages.value = []
  nextCursor.value = ''
  error.value = ''
  await loadMessages(false)
}

async function loadMessages(append: boolean) {
  const sourceID = selectedSourceID.value
  if (!sourceID || offline.value || (append && !nextCursor.value)) return
  if (append) loadingMore.value = true
  else messagesLoading.value = true
  error.value = ''
  try {
    const page = await api.emailMessages(sourceID, append ? nextCursor.value : '', 20)
    if (selectedSourceID.value !== sourceID) return
    messages.value = append ? [...messages.value, ...page.items] : page.items
    nextCursor.value = page.next_cursor ?? ''
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '邮件归档加载失败，请稍后重试'
  } finally {
    messagesLoading.value = false
    loadingMore.value = false
  }
}

async function registerSource() {
  if (!canManage.value || offline.value) return
  creating.value = true
  error.value = ''
  if (!registrationKey.value) registrationKey.value = crypto.randomUUID()
  try {
    const created = await api.registerEmailSource(
      {
        display_name: displayName.value,
        mailbox_address: mailboxAddress.value,
        imap_host: imapHost.value,
        imap_port: imapPort.value,
        transport_security: transportSecurity.value,
      },
      registrationKey.value,
    )
    displayName.value = ''
    mailboxAddress.value = ''
    imapHost.value = ''
    imapPort.value = 993
    transportSecurity.value = 'implicit_tls'
    registrationKey.value = ''
    showRegistration.value = false
    await loadSources(created.id)
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '邮箱来源登记失败，请稍后重试'
  } finally {
    creating.value = false
  }
}

function setOnlineState() {
  offline.value = !navigator.onLine
  if (!offline.value && canRead.value) void loadSources(selectedSourceID.value)
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.valueOf())) return '时间未知'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(date)
}

onMounted(() => {
  if (canRead.value) void loadSources()
  else loading.value = false
  window.addEventListener('online', setOnlineState)
  window.addEventListener('offline', setOnlineState)
})

onUnmounted(() => {
  window.removeEventListener('online', setOnlineState)
  window.removeEventListener('offline', setOnlineState)
})
</script>

<template>
  <div class="page-stack email-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <span>来源</span><span aria-hidden="true">/</span><strong>邮箱来源</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>邮箱来源</h1>
        <p>登记无凭据连接描述符，查看本地归档邮件与逐附件处理结果。</p>
      </div>
      <button
        v-if="canManage"
        class="button button-primary email-register-toggle"
        type="button"
        :aria-expanded="showRegistration"
        :disabled="offline"
        @click="showRegistration = !showRegistration"
      >
        {{ showRegistration ? '收起登记表单' : '登记邮箱来源' }}
      </button>
    </header>

    <div v-if="!canRead" class="notice notice-danger" role="alert">
      <span aria-hidden="true">!</span>
      <span>当前账号没有读取邮箱归档的权限。</span>
    </div>
    <template v-else>
      <div v-if="offline" class="notice notice-warning" role="status">
        <span aria-hidden="true">!</span>
        <span>当前离线。已显示的本地归档摘要仍保留，恢复网络后会重新加载。</span>
      </div>
      <div v-if="error" class="notice notice-danger" role="alert">
        <span aria-hidden="true">!</span><span>{{ error }}</span>
        <button class="text-button" type="button" :disabled="offline" @click="loadSources()">
          重试
        </button>
      </div>

      <section
        v-if="canManage && showRegistration"
        class="panel email-registration-panel"
        aria-labelledby="email-registration-title"
      >
        <div class="panel-heading">
          <div>
            <h2 id="email-registration-title">登记无凭据描述符</h2>
            <p>保存后状态为“待连接”；本切片不会连接邮箱或立即同步。</p>
          </div>
        </div>
        <form class="email-registration-form" @submit.prevent="registerSource">
          <label class="field-stack">
            <span>显示名称</span>
            <input v-model.trim="displayName" class="input" type="text" maxlength="100" required />
          </label>
          <label class="field-stack">
            <span>邮箱地址</span>
            <input
              v-model.trim="mailboxAddress"
              class="input"
              type="email"
              maxlength="254"
              autocomplete="off"
              required
            />
          </label>
          <label class="field-stack">
            <span>IMAP 主机</span>
            <input
              v-model.trim="imapHost"
              class="input"
              type="text"
              maxlength="253"
              autocomplete="off"
              required
            />
          </label>
          <label class="field-stack">
            <span>IMAP 端口</span>
            <input
              v-model.number="imapPort"
              class="input"
              type="number"
              min="1"
              max="65535"
              inputmode="numeric"
              required
            />
          </label>
          <label class="field-stack">
            <span>传输安全</span>
            <select v-model="transportSecurity" class="select">
              <option value="implicit_tls">隐式 TLS</option>
              <option value="starttls">STARTTLS</option>
            </select>
          </label>
          <div class="email-registration-action">
            <p class="form-note">这里只保存连接身份，不验证外部账号，也不产生同步成功状态。</p>
            <button class="button button-primary" type="submit" :disabled="creating || offline">
              {{ creating ? '正在登记…' : '保存来源描述符' }}
            </button>
          </div>
        </form>
      </section>

      <div v-if="loading" class="panel state-layout" role="status">
        <span class="spinner spinner-large" aria-hidden="true"></span>
        <strong>正在读取邮箱来源</strong>
        <span>只加载当前租户的本地归档摘要。</span>
      </div>

      <div v-else-if="sources.length === 0" class="panel state-layout">
        <span class="state-glyph" aria-hidden="true">邮</span>
        <strong>还没有邮箱来源</strong>
        <span>{{
          canManage ? '登记一个无凭据描述符，准备后续连接。' : '请联系 Owner 登记来源。'
        }}</span>
      </div>

      <div v-else class="email-layout">
        <aside class="panel email-source-panel" aria-labelledby="email-source-list-title">
          <div class="panel-heading">
            <div>
              <h2 id="email-source-list-title">来源列表</h2>
              <p>{{ sources.length }} 个来源</p>
            </div>
          </div>
          <ul class="email-source-list">
            <li v-for="source in sources" :key="source.id">
              <button
                type="button"
                :aria-current="selectedSourceID === source.id ? 'true' : undefined"
                @click="selectSource(source.id)"
              >
                <span class="email-source-heading">
                  <strong>{{ source.display_name }}</strong>
                  <span class="status" :data-tone="emailSourceStatusMeta[source.status].tone">
                    <span aria-hidden="true">●</span
                    >{{ emailSourceStatusMeta[source.status].label }}
                  </span>
                </span>
                <span>{{ source.mailbox_address }}</span>
                <small
                  >{{ source.imap_host }}:{{ source.imap_port }} ·
                  {{ source.transport_security }}</small
                >
                <small>
                  邮件 {{ source.message_count }} · 附件 {{ source.attachment_count }} · 阻断
                  {{ source.blocked_count }}
                </small>
              </button>
            </li>
          </ul>
        </aside>

        <section class="panel email-message-panel" aria-labelledby="email-message-list-title">
          <div class="panel-heading email-message-heading">
            <div>
              <h2 id="email-message-list-title">{{ selectedSource?.display_name }}的本地邮件</h2>
              <p v-if="selectedSource?.status === 'pending_connection'">
                描述符已保存，尚未建立真实邮箱连接。
              </p>
              <p v-else>这里只显示安全头投影与附件结果，不渲染邮件正文。</p>
            </div>
            <span v-if="selectedSource" class="quiet">本页 {{ messages.length }} 封</span>
          </div>

          <div v-if="messagesLoading" class="state-layout compact" role="status">
            <span class="spinner spinner-large" aria-hidden="true"></span>
            <strong>正在读取本地邮件归档</strong>
          </div>
          <div v-else-if="messages.length === 0" class="state-layout compact">
            <span class="state-glyph" aria-hidden="true">信</span>
            <strong>这个来源还没有本地邮件</strong>
            <span>没有同步按钮；真实邮箱连接仍在独立联调门禁之外。</span>
          </div>
          <ol v-else class="email-message-list">
            <li v-for="message in messages" :key="message.id" class="email-message-card">
              <header>
                <div>
                  <strong>{{ message.subject || '（无主题）' }}</strong>
                  <span>{{ message.sender_address || '发件人未提供' }}</span>
                </div>
                <span class="status" :data-tone="emailMessageStatusMeta[message.status].tone">
                  <span aria-hidden="true">●</span
                  >{{ emailMessageStatusMeta[message.status].label }}
                </span>
              </header>
              <div class="email-message-meta">
                <time :datetime="message.received_at"
                  >接收 {{ formatDate(message.received_at) }}</time
                >
                <span v-if="message.sent_at">发送 {{ formatDate(message.sent_at) }}</span>
                <a
                  class="button button-small"
                  :href="api.emailMessageDownloadURL(message.id)"
                  download
                >
                  下载原始邮件
                </a>
              </div>
              <p v-if="message.status === 'blocked'" class="email-blocked-reason" role="status">
                {{ message.safe_error_text || '邮件结构无法安全解析' }}
              </p>
              <div v-if="message.attachments.length === 0" class="email-no-attachments">
                没有可单独下载的附件
              </div>
              <ul v-else class="email-attachment-list">
                <li v-for="attachment in message.attachments" :key="attachment.id">
                  <div class="email-attachment-main">
                    <strong>{{ attachment.original_name }}</strong>
                    <span>
                      {{ formatArchiveBytes(attachment.size_bytes) }} ·
                      {{ attachment.declared_mime }}
                    </span>
                    <small v-if="attachment.safe_reason_code">
                      {{ attachmentReasonLabel(attachment.safe_reason_code) }}
                    </small>
                  </div>
                  <span
                    class="status"
                    :data-tone="emailAttachmentStatusMeta[attachment.processing_status].tone"
                  >
                    <span aria-hidden="true">●</span
                    >{{ emailAttachmentStatusMeta[attachment.processing_status].label }}
                  </span>
                  <div class="email-attachment-actions">
                    <RouterLink v-if="attachment.document_id" class="text-button" to="/inbox">
                      查看收件箱
                    </RouterLink>
                    <a
                      v-if="attachment.size_bytes > 0"
                      class="text-button"
                      :href="api.emailAttachmentDownloadURL(attachment.id)"
                      download
                    >
                      下载附件
                    </a>
                  </div>
                </li>
              </ul>
            </li>
          </ol>
          <div v-if="nextCursor" class="email-pagination">
            <button
              class="button"
              type="button"
              :disabled="loadingMore || offline"
              @click="loadMessages(true)"
            >
              {{ loadingMore ? '正在加载…' : '加载更多邮件' }}
            </button>
          </div>
        </section>
      </div>
    </template>
  </div>
</template>
