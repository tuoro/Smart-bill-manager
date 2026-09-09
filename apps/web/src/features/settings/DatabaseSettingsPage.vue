<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ApiError, api } from '../../data/client'
import AppIcon from '../../components/AppIcon.vue'

const loading = ref(true)
const editable = ref(false)
const reason = ref('')
const host = ref('')
const port = ref('5432')
const database = ref('')
const user = ref('')
const password = ref('')
const pending = ref(false)
const testing = ref(false)
const error = ref('')
const notice = ref('')
const saved = ref(false)

onMounted(async () => {
  try {
    const current = await api.databaseSettings()
    editable.value = current.editable
    reason.value = current.reason ?? ''
    host.value = current.host
    port.value = String(current.port)
    database.value = current.database
    user.value = current.user
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '读取数据库设置失败'
  } finally {
    loading.value = false
  }
})

function payload() {
  return {
    host: host.value,
    port: port.value,
    database: database.value,
    user: user.value,
    password: password.value,
    ssl_mode: 'disable',
  }
}

async function test() {
  if (testing.value || pending.value) return
  error.value = ''
  notice.value = ''
  testing.value = true
  try {
    await api.saveDatabaseSettings(payload(), true)
    notice.value = '连接成功。'
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '连接检测失败'
  } finally {
    testing.value = false
  }
}

async function save() {
  if (pending.value || testing.value) return
  error.value = ''
  notice.value = ''
  pending.value = true
  try {
    await api.saveDatabaseSettings(payload())
    password.value = ''
    saved.value = true
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '保存失败'
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <section class="page-section">
    <header class="page-heading">
      <h1>数据库连接</h1>
      <p>应用连接 PostgreSQL 所用的地址与账号。修改后需要重启应用容器才会生效。</p>
    </header>

    <p v-if="loading">正在读取…</p>

    <template v-else>
      <div v-if="error" class="notice notice-danger" role="alert">
        <AppIcon name="alert" /><span>{{ error }}</span>
      </div>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>

      <div v-if="saved" class="notice notice-stack" role="status">
        <strong>已保存，请重启应用容器</strong>
        <p>
          现有连接仍在使用旧配置。执行
          <code>docker restart smart-bill-manager</code> 后新配置生效。
        </p>
      </div>

      <p v-if="!editable" class="notice">
        {{ reason || '当前连接由环境变量固定，页面不可修改。' }}
      </p>

      <form class="stack-form" @submit.prevent="save">
        <label class="field-stack">
          <span>数据库地址</span>
          <small class="field-hint">同一 Docker 网络内填容器名；其他情况填 IP 或主机名。</small>
          <input v-model.trim="host" class="input" required :disabled="!editable || pending" />
        </label>
        <label class="field-stack">
          <span>端口</span>
          <input v-model.trim="port" class="input" required :disabled="!editable || pending" />
        </label>
        <label class="field-stack">
          <span>数据库名称</span>
          <input v-model.trim="database" class="input" required :disabled="!editable || pending" />
        </label>
        <label class="field-stack">
          <span>数据库账号</span>
          <input v-model.trim="user" class="input" required :disabled="!editable || pending" />
        </label>
        <label class="field-stack">
          <span>数据库密码</span>
          <small class="field-hint">出于安全考虑不回显，保存时必须重新输入。</small>
          <input
            v-model="password"
            class="input"
            type="password"
            autocomplete="off"
            required
            :disabled="!editable || pending"
          />
        </label>
        <div v-if="editable" class="database-actions">
          <button class="button" type="button" :disabled="pending || testing" @click="test">
            {{ testing ? '正在检测…' : '检测连接' }}
          </button>
          <button class="button button-primary" type="submit" :disabled="pending || testing">
            {{ pending ? '正在保存…' : '保存' }}
          </button>
        </div>
      </form>
    </template>
  </section>
</template>

<style scoped>
.database-actions {
  display: flex;
  gap: 12px;
}
.field-hint {
  margin-top: -2px;
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 400;
}
</style>
