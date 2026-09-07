<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError, api } from '../../data/client'
import { theme, toggleTheme } from '../../app/theme'
import AppIcon from '../../components/AppIcon.vue'

const router = useRouter()
const identifier = ref('')
const name = ref('管理员')
const tenant = ref('我的工作区')
const currency = ref('CNY')
const timezone = ref(Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai')
const password = ref('')
const confirmation = ref('')
const showPassword = ref(false)
const pending = ref(false)
const error = ref('')
const ready = ref(false)
const advanced = ref(false)
const stage = ref<'database' | 'account'>('account')
const currencies = ['CNY', 'USD', 'EUR', 'JPY']

const dbHost = ref('database')
const dbPort = ref('5432')
const dbName = ref('smart_bill_manager')
const dbUser = ref('')
const dbPassword = ref('')
const testing = ref(false)
const tested = ref('')

function databasePayload() {
  return {
    host: dbHost.value,
    port: dbPort.value,
    database: dbName.value,
    user: dbUser.value,
    password: dbPassword.value,
    ssl_mode: 'disable',
  }
}

async function testDatabase() {
  if (testing.value || pending.value) return
  error.value = ''
  tested.value = ''
  testing.value = true
  try {
    await api.testDatabase(databasePayload())
    tested.value = '连接成功，可以继续。'
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '连接检测失败，请检查后重试'
  } finally {
    testing.value = false
  }
}

onMounted(async () => {
  try {
    const state = await api.setupRequired()
    if (!state.required) {
      void router.replace({ name: 'login' })
      return
    }
    stage.value = state.database_required ? 'database' : 'account'
    ready.value = true
  } catch {
    error.value = '无法读取初始化状态，请稍后刷新页面'
  }
})

watch([dbHost, dbPort, dbName, dbUser, dbPassword], () => {
  tested.value = ''
})

async function submitDatabase() {
  if (pending.value) return
  error.value = ''
  pending.value = true
  try {
    await api.configureDatabase(databasePayload())
    dbPassword.value = ''
    // 服务端随即转入常规启动，短暂不可用；轮询直到进入创建管理员这一步。
    await waitForAccountStage()
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '数据库连接失败，请检查后重试'
  } finally {
    pending.value = false
  }
}

async function waitForAccountStage() {
  for (let attempt = 0; attempt < 40; attempt++) {
    await new Promise((resolve) => setTimeout(resolve, 500))
    try {
      const state = await api.setupRequired()
      if (!state.database_required) {
        if (!state.required) {
          void router.replace({ name: 'login' })
          return
        }
        stage.value = 'account'
        return
      }
    } catch {
      // 重启窗口内的请求失败属于预期，继续等待。
    }
  }
  error.value = '数据库已保存，但服务重启超时。请刷新页面查看状态。'
}

async function submit() {
  if (pending.value) return
  error.value = ''
  if (password.value !== confirmation.value) {
    error.value = '两次输入的密码不一致'
    return
  }
  if (new TextEncoder().encode(password.value).length < 12) {
    error.value = '密码至少需要 12 字节'
    return
  }
  pending.value = true
  try {
    await api.createOwner({
      email: identifier.value,
      password: password.value,
      display_name: name.value,
      tenant_name: tenant.value,
      default_currency: currency.value,
      timezone: timezone.value,
    })
    password.value = ''
    confirmation.value = ''
    void router.replace({ name: 'login', query: { reason: 'setup_complete' } })
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '初始化失败，请重试'
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <!-- 复用登录页的认证外壳（.login-* 为全局样式），保证两页视觉一致。 -->
  <main id="main-content" class="login-page" tabindex="-1">
    <header class="login-topbar">
      <span class="brand" aria-label="智能账单管理">
        <span class="brand-mark"><AppIcon name="receipt" /></span>
        <span class="brand-name">智能账单管理</span>
      </span>
      <button
        class="icon-button"
        type="button"
        :aria-label="theme === 'light' ? '切换到深色模式' : '切换到浅色模式'"
        @click="toggleTheme"
      >
        <AppIcon :name="theme === 'light' ? 'moon' : 'sun'" />
      </button>
    </header>

    <div class="login-layout">
      <section class="login-story" aria-labelledby="setup-story-title">
        <p class="eyebrow">一次性初始化</p>
        <h1 id="setup-story-title">
          {{ stage === 'database' ? '连接你的数据库' : '创建你的工作区' }}
        </h1>
        <p class="login-intro">
          {{
            stage === 'database'
              ? '这台部署还没有连接数据库。填写连接信息后，系统会自动建表并进入下一步。'
              : '这台部署还没有账号。现在创建的是管理员账号，完成后本页面将永久关闭。'
          }}
        </p>
        <ol v-if="stage === 'database'" class="trace-flow" aria-label="初始化步骤">
          <li><strong>连接数据库</strong><span>验证通过后保存在服务器本地</span></li>
          <li><strong>自动建表</strong><span>无需手工执行任何 SQL</span></li>
          <li><strong>创建管理员</strong><span>下一步设置登录用的用户名和密码</span></li>
        </ol>
        <ol v-else class="trace-flow" aria-label="初始化说明">
          <li><strong>管理员</strong><span>拥有全部权限，可邀请成员并分配角色</span></li>
          <li><strong>工作区</strong><span>你的账单、行程和报销都归属于它</span></li>
          <li><strong>只有一次</strong><span>创建后无法再通过本页添加账号</span></li>
        </ol>
      </section>

      <section class="login-form-area" aria-labelledby="setup-title">
        <p v-if="!ready && !error" class="login-form">正在检查初始化状态…</p>

        <form
          v-else-if="ready && stage === 'database'"
          class="login-form"
          @submit.prevent="submitDatabase"
        >
          <div>
            <h2 id="setup-title">连接 PostgreSQL</h2>
            <p>填写上一步创建 PostgreSQL 时用的账号和密码。</p>
          </div>

          <div v-if="error" class="notice notice-danger" role="alert">
            <AppIcon name="alert" />
            <span>{{ error }}</span>
          </div>

          <label class="field-stack">
            <span>数据库地址</span>
            <small class="field-hint"
              >同一 Docker 网络内填数据库容器名；本机其他实例填 IP 或主机名。</small
            >
            <input v-model.trim="dbHost" class="input" required :disabled="pending" />
          </label>
          <label class="field-stack">
            <span>端口</span>
            <small class="field-hint">PostgreSQL 默认 5432，没改过就不用动。</small>
            <input v-model.trim="dbPort" class="input" required :disabled="pending" />
          </label>
          <label class="field-stack">
            <span>数据库名称</span>
            <small class="field-hint"
              >创建容器时 <code>POSTGRES_DB</code> 设的值。需要已经存在，且该账号能建表。</small
            >
            <input v-model.trim="dbName" class="input" required :disabled="pending" />
          </label>
          <label class="field-stack">
            <span>数据库账号</span>
            <small class="field-hint"
              >创建 PostgreSQL 容器时 <code>POSTGRES_USER</code> 设的值。</small
            >
            <input
              v-model.trim="dbUser"
              class="input"
              autocomplete="off"
              required
              :disabled="pending"
            />
          </label>
          <label class="field-stack">
            <span>数据库密码</span>
            <small class="field-hint"
              >同上，<code>POSTGRES_PASSWORD</code> 的值。它和下一步的管理员密码无关。</small
            >
            <input
              v-model="dbPassword"
              class="input"
              type="password"
              autocomplete="off"
              required
              :disabled="pending"
            />
          </label>
          <p v-if="tested" class="notice" role="status">{{ tested }}</p>

          <div class="setup-actions">
            <button
              class="button button-secondary"
              type="button"
              :disabled="pending || testing"
              @click="testDatabase"
            >
              <span v-if="testing" class="spinner" aria-hidden="true"></span>
              {{ testing ? '正在检测…' : '检测连接' }}
            </button>
            <button
              class="button button-primary login-submit"
              type="submit"
              :disabled="pending || testing"
            >
              <span v-if="pending" class="spinner" aria-hidden="true"></span>
              {{ pending ? '正在连接…' : '保存并继续' }}
            </button>
          </div>
          <p class="login-security">
            <AppIcon name="shield" /> 检测不会保存任何设置；保存时也会再验证一次，失败则不写入。
          </p>
        </form>

        <form v-else-if="ready" class="login-form" @submit.prevent="submit">
          <div>
            <h2 id="setup-title">创建管理员账号</h2>
            <p>这是本机自托管的部署，下面的信息只保存在你自己的数据库里。</p>
          </div>

          <div v-if="error" id="setup-error" class="notice notice-danger" role="alert">
            <AppIcon name="alert" />
            <span>{{ error }}</span>
          </div>

          <label class="field-stack">
            <span>管理员用户名</span>
            <small class="field-hint"
              >登录时用的账号名。本系统不发送任何邮件，可以直接用 admin；填邮箱也可以。</small
            >
            <input
              v-model.trim="identifier"
              class="input"
              type="text"
              autocomplete="username"
              maxlength="254"
              placeholder="admin"
              required
              :disabled="pending"
              :aria-invalid="Boolean(error)"
              :aria-describedby="error ? 'setup-error' : undefined"
            />
          </label>

          <label class="field-stack">
            <span>设置密码</span>
            <small class="field-hint">至少 12 个字符。忘记后只能通过服务器上的命令行恢复。</small>
            <span class="password-control">
              <input
                v-model="password"
                class="input"
                aria-label="设置密码"
                :type="showPassword ? 'text' : 'password'"
                autocomplete="new-password"
                maxlength="1024"
                required
                :disabled="pending"
              />
              <button
                class="password-toggle"
                type="button"
                :aria-label="showPassword ? '隐藏密码' : '显示密码'"
                @click="showPassword = !showPassword"
              >
                {{ showPassword ? '隐藏' : '显示' }}
              </button>
            </span>
          </label>

          <label class="field-stack">
            <span>确认密码</span>
            <input
              v-model="confirmation"
              class="input"
              :type="showPassword ? 'text' : 'password'"
              autocomplete="new-password"
              maxlength="1024"
              required
              :disabled="pending"
            />
          </label>

          <button
            class="setup-more"
            type="button"
            :aria-expanded="advanced"
            @click="advanced = !advanced"
          >
            <span class="setup-more-mark" :class="{ 'is-open': advanced }">
              <AppIcon name="chevron-right" />
            </span>
            <span>更多设置（姓名、工作区名称、币种、时区）</span>
          </button>

          <template v-if="advanced">
            <label class="field-stack">
              <span>姓名</span>
              <small class="field-hint">显示在界面右上角和成员列表里，用来区分是谁。</small>
              <input
                v-model.trim="name"
                class="input"
                maxlength="100"
                autocomplete="name"
                required
                :disabled="pending"
              />
            </label>
            <label class="field-stack">
              <span>工作区名称</span>
              <small class="field-hint">显示在界面左上角，你的账单、行程和报销都归属于它。</small>
              <input v-model.trim="tenant" class="input" maxlength="120" :disabled="pending" />
            </label>
            <label class="field-stack">
              <span>默认币种</span>
              <small class="field-hint">新建单据时的默认币种，每张单据仍可单独修改。</small>
              <select v-model="currency" class="input" :disabled="pending">
                <option v-for="item in currencies" :key="item" :value="item">{{ item }}</option>
              </select>
            </label>
            <label class="field-stack">
              <span>时区</span>
              <small class="field-hint">用于判断单据日期和跨期分配，已按浏览器时区预填。</small>
              <input
                v-model.trim="timezone"
                class="input"
                maxlength="64"
                required
                :disabled="pending"
              />
            </label>
          </template>

          <button class="button button-primary login-submit" type="submit" :disabled="pending">
            <span v-if="pending" class="spinner" aria-hidden="true"></span>
            {{ pending ? '正在创建…' : '创建并进入' }}
          </button>
          <p class="login-security">
            <AppIcon name="shield" /> 密码只提交到本机服务，不写入配置文件或日志。
          </p>
        </form>
      </section>
    </div>
  </main>
</template>

<style scoped>
.setup-actions {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 12px;
}

.field-hint {
  margin-top: -2px;
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 400;
  line-height: 1.6;
}

.setup-more {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: -4px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--brand);
  cursor: pointer;
  font: inherit;
}

.setup-more-mark {
  display: inline-flex;
  transition: transform 0.15s ease;
}

.setup-more-mark.is-open {
  transform: rotate(90deg);
}

@media (prefers-reduced-motion: reduce) {
  .setup-more-mark {
    transition: none;
  }
}
</style>
