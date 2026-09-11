<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { sessionStore } from '../../app/session'
import AppIcon from '../../components/AppIcon.vue'
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
  emailConnectionStatusMeta,
  emailMessageStatusMeta,
  formatArchiveBytes,
} from './model'
import { formatSystemTime } from '../facts/time'
import { randomUUID } from '../../data/random'

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
const imapUsername = ref('')
const imapPassword = ref('')
const registrationKey = ref('')
// 连接动作：同一时间只跑一个，按钮上显示进行中。
const busyAction = ref('')
const credentialsOpen = ref(false)
const newUsername = ref('')
const newPassword = ref('')

const selectedSource = computed(
  () => sources.value.find((source) => source.id === selectedSourceID.value) ?? null,
)
// 邮箱是每个人自己的：登记人和管理员能管，其他成员根本看不到（后端已过滤）。
const canOperate = computed(
  () =>
    canManage.value &&
    !!selectedSource.value &&
    (session.value?.role === 'owner' ||
      selectedSource.value.created_by_user_id === session.value?.user.id),
)

watch([displayName, mailboxAddress, imapHost, imapPort, transportSecurity, imapUsername], () => {
  registrationKey.value = ''
})

function replaceSource(updated: EmailSource) {
  sources.value = sources.value.map((source) => (source.id === updated.id ? updated : source))
}

async function runAction(action: 'detect' | 'activate' | 'deactivate' | 'sync') {
  const source = selectedSource.value
  if (!source || busyAction.value || offline.value) return
  busyAction.value = action
  error.value = ''
  try {
    const updated = await api.emailSourceAction(source.id, action)
    replaceSource(updated)
    if (action === 'sync' && selectedSourceID.value === source.id) await loadMessages(false)
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '操作失败，请稍后重试'
  } finally {
    busyAction.value = ''
  }
}

async function saveCredentials() {
  const source = selectedSource.value
  if (!source || busyAction.value || !newPassword.value) return
  busyAction.value = 'credentials'
  error.value = ''
  const password = newPassword.value
  newPassword.value = ''
  try {
    replaceSource(
      await api.setEmailSourceCredentials(source.id, newUsername.value.trim(), password),
    )
    credentialsOpen.value = false
    newUsername.value = ''
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '保存密码失败，请稍后重试'
  } finally {
    busyAction.value = ''
  }
}

async function removeSource() {
  const source = selectedSource.value
  if (!source || busyAction.value) return
  if (
    !window.confirm(
      `确定删除「${source.display_name}」？将停止同步并清除密码；已归档的邮件和由此生成的单据保留。`,
    )
  )
    return
  busyAction.value = 'delete'
  error.value = ''
  try {
    await api.deleteEmailSource(source.id)
    sources.value = sources.value.filter((item) => item.id !== source.id)
    credentialsOpen.value = false
    selectedSourceID.value = sources.value[0]?.id ?? ''
    messages.value = []
    nextCursor.value = ''
    if (selectedSourceID.value) await loadMessages(false)
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '删除失败，请稍后重试'
  } finally {
    busyAction.value = ''
  }
}

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
  if (!registrationKey.value) registrationKey.value = randomUUID()
  const password = imapPassword.value
  try {
    const created = await api.registerEmailSource(
      {
        display_name: displayName.value,
        mailbox_address: mailboxAddress.value,
        imap_host: imapHost.value,
        imap_port: imapPort.value,
        transport_security: transportSecurity.value,
        imap_username: imapUsername.value,
        imap_password: password,
      },
      registrationKey.value,
    )
    displayName.value = ''
    mailboxAddress.value = ''
    imapHost.value = ''
    imapPort.value = 993
    transportSecurity.value = 'implicit_tls'
    imapUsername.value = ''
    registrationKey.value = ''
    showRegistration.value = false
    await loadSources(created.id)
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '邮箱来源登记失败，请稍后重试'
  } finally {
    imapPassword.value = ''
    creating.value = false
  }
}

function setOnlineState() {
  offline.value = !navigator.onLine
  if (!offline.value && canRead.value) void loadSources(selectedSourceID.value)
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
        <p>管理邮箱来源，查看已归档的邮件和附件处理结果。</p>
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
      <AppIcon name="lock" />
      <span>当前账号没有读取邮箱归档的权限。</span>
    </div>
    <template v-else>
      <div v-if="offline" class="notice notice-warning" role="status">
        <AppIcon name="alert" />
        <span>当前离线。已显示的本地归档摘要仍保留，恢复网络后会重新加载。</span>
      </div>
      <div v-if="error" class="notice notice-danger" role="alert">
        <AppIcon name="alert" /><span>{{ error }}</span>
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
            <h2 id="email-registration-title">登记邮箱</h2>
            <p>保存后先「检测连接」，通过再「开启同步」，新邮件里的票据会自动进入识别队列。</p>
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
            <span>邮箱服务器（IMAP）</span>
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
            <span>加密方式</span>
            <select v-model="transportSecurity" class="select">
              <option value="implicit_tls">隐式 TLS</option>
              <option value="starttls">STARTTLS</option>
            </select>
          </label>
          <label class="field-stack">
            <span>登录用户名（可选）</span>
            <input
              v-model.trim="imapUsername"
              class="input"
              type="text"
              maxlength="254"
              autocomplete="off"
              placeholder="留空则用邮箱地址登录"
            />
          </label>
          <label class="field-stack">
            <span>密码或授权码</span>
            <input
              v-model="imapPassword"
              class="input"
              type="password"
              maxlength="1024"
              autocomplete="new-password"
              required
            />
          </label>
          <div class="email-registration-action">
            <p class="form-note">
              密码加密保存，不回显。QQ、163、Gmail 等邮箱需先在邮箱设置里开启
              IMAP，并用「授权码」代替登录密码。
            </p>
            <button class="button button-primary" type="submit" :disabled="creating || offline">
              {{ creating ? '正在登记…' : '保存邮箱来源' }}
            </button>
          </div>
        </form>
      </section>

      <div v-if="loading" class="panel state-layout" role="status">
        <span class="spinner spinner-large" aria-hidden="true"></span>
        <strong>正在读取邮箱来源</strong>
        <span>正在读取当前工作区的来源列表。</span>
      </div>

      <div v-else-if="sources.length === 0" class="panel state-layout">
        <span class="state-glyph"><AppIcon name="mail" /></span>
        <strong>还没有邮箱来源</strong>
        <span>{{
          canManage ? '先登记邮箱信息，已归档的邮件会显示在这里。' : '请联系管理员登记邮箱来源。'
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
                  <span
                    class="status"
                    :data-tone="emailConnectionStatusMeta[source.connection_status].tone"
                  >
                    <span aria-hidden="true">●</span
                    >{{ emailConnectionStatusMeta[source.connection_status].label }}
                  </span>
                  <span v-if="source.sync_enabled" class="status" data-tone="info"
                    ><span aria-hidden="true">●</span>同步中</span
                  >
                </span>
                <span>{{ source.mailbox_address }}</span>
                <small
                  >{{ source.imap_host }}:{{ source.imap_port }} ·
                  {{
                    source.transport_security === 'implicit_tls' ? '隐式 TLS' : 'STARTTLS'
                  }}</small
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
              <h2 id="email-message-list-title">{{ selectedSource?.display_name }}</h2>
              <p v-if="selectedSource">
                {{ emailConnectionStatusMeta[selectedSource.connection_status].label
                }}<template v-if="selectedSource.connection_message">
                  · {{ selectedSource.connection_message }}</template
                ><template v-if="selectedSource.sync_enabled"> · 每 5 分钟自动同步</template
                ><template v-if="selectedSource.last_sync_at">
                  · 上次同步 {{ formatSystemTime(selectedSource.last_sync_at) }}
                  {{ selectedSource.last_sync_message }}</template
                >
              </p>
            </div>
            <span v-if="selectedSource" class="quiet">本页 {{ messages.length }} 封</span>
          </div>
          <div v-if="selectedSource && canOperate" class="email-connection-actions">
            <button
              class="button button-small"
              type="button"
              :disabled="Boolean(busyAction) || offline || !selectedSource.has_password"
              @click="runAction('detect')"
            >
              {{ busyAction === 'detect' ? '检测中…' : '检测连接' }}
            </button>
            <button
              v-if="!selectedSource.sync_enabled"
              class="button button-small button-primary"
              type="button"
              :disabled="
                Boolean(busyAction) || offline || selectedSource.connection_status !== 'passed'
              "
              @click="runAction('activate')"
            >
              开启同步
            </button>
            <button
              v-else
              class="button button-small"
              type="button"
              :disabled="Boolean(busyAction) || offline"
              @click="runAction('deactivate')"
            >
              停止同步
            </button>
            <button
              class="button button-small"
              type="button"
              :disabled="
                Boolean(busyAction) || offline || selectedSource.connection_status !== 'passed'
              "
              @click="runAction('sync')"
            >
              {{ busyAction === 'sync' ? '同步中…' : '立即同步' }}
            </button>
            <button
              class="button button-small"
              type="button"
              :disabled="Boolean(busyAction) || offline"
              :aria-expanded="credentialsOpen"
              @click="credentialsOpen = !credentialsOpen"
            >
              {{ selectedSource.has_password ? '更换密码' : '填写密码' }}
            </button>
            <button
              class="button button-small button-danger"
              type="button"
              :disabled="Boolean(busyAction) || offline"
              @click="removeSource"
            >
              删除邮箱
            </button>
          </div>
          <form
            v-if="selectedSource && canOperate && credentialsOpen"
            class="email-credentials-form"
            @submit.prevent="saveCredentials"
          >
            <label class="field-stack">
              <span>登录用户名（可选）</span>
              <input
                v-model="newUsername"
                class="input"
                type="text"
                maxlength="254"
                autocomplete="off"
                :placeholder="selectedSource.imap_username || '留空则用邮箱地址登录'"
              />
            </label>
            <label class="field-stack">
              <span>新密码或授权码</span>
              <input
                v-model="newPassword"
                class="input"
                type="password"
                maxlength="1024"
                autocomplete="new-password"
                required
              />
            </label>
            <p class="form-note">保存后连接回到待检测、同步暂停，需要重新检测并开启。</p>
            <button class="button button-primary" type="submit" :disabled="Boolean(busyAction)">
              {{ busyAction === 'credentials' ? '正在保存…' : '保存密码' }}
            </button>
          </form>

          <div v-if="messagesLoading" class="state-layout compact" role="status">
            <span class="spinner spinner-large" aria-hidden="true"></span>
            <strong>正在读取本地邮件归档</strong>
          </div>
          <div v-else-if="messages.length === 0" class="state-layout compact">
            <span class="state-glyph"><AppIcon name="mail" /></span>
            <strong>这个邮箱还没有同步到邮件</strong>
            <span>{{
              selectedSource?.connection_status === 'passed'
                ? selectedSource.sync_enabled
                  ? '首次同步只回溯最近 30 天，有新邮件会自动出现在这里。'
                  : '连接已通过，开启同步或点「立即同步」拉取最近 30 天的邮件。'
                : '先检测连接，通过后才能同步。'
            }}</span>
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
                  >接收 {{ formatSystemTime(message.received_at) }}</time
                >
                <span v-if="message.sent_at">发送 {{ formatSystemTime(message.sent_at) }}</span>
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
