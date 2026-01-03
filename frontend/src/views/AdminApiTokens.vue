<template>
  <div class="page">
    <Card class="sbm-surface">
      <template #title>
        <div class="header">
          <span>API Token</span>
          <div class="toolbar">
            <InputText v-model="tokenName" placeholder="名称（可选）" class="name-input" />
            <Dropdown
              v-model="expiresInDays"
              :options="expiresOptions"
              optionLabel="label"
              optionValue="value"
              class="expires-dropdown"
            />
            <Button icon="pi pi-plus" label="创建" :loading="creating" @click="createToken" />
          </div>
        </div>
      </template>
      <template #content>
        <Message v-if="!isAdmin" severity="warn" :closable="false">仅管理员可访问</Message>
        <div v-else class="content">
          <DataTable
            class="tokens-table"
            :value="tokens"
            :loading="loading"
            responsiveLayout="scroll"
            :paginator="true"
            :rows="20"
            :rowsPerPageOptions="[10, 20, 50]"
            dataKey="id"
          >
            <Column field="name" header="名称" :style="{ width: '18%' }" />
            <Column field="token_hint" header="Token" :style="{ width: '18%' }" />
            <Column field="created_at" header="创建时间" :style="{ width: '20%' }">
              <template #body="{ data: row }">{{ formatDateTime(row.created_at) }}</template>
            </Column>
            <Column field="expires_at" header="过期时间" :style="{ width: '20%' }">
              <template #body="{ data: row }">
                <span v-if="row.expires_at">{{ formatDateTime(row.expires_at) }}</span>
                <span v-else class="muted">不过期</span>
              </template>
            </Column>
            <Column field="last_used_at" header="最后使用" :style="{ width: '18%' }">
              <template #body="{ data: row }">
                <span v-if="row.last_used_at">{{ formatDateTime(row.last_used_at) }}</span>
                <span v-else class="muted">-</span>
              </template>
            </Column>
            <Column header="状态" :style="{ width: '12%' }">
              <template #body="{ data: row }">
                <Tag v-if="row.revoked_at" severity="secondary" value="已撤销" />
                <Tag v-else-if="isExpired(row.expires_at)" severity="danger" value="已过期" />
                <Tag v-else severity="success" value="可用" />
              </template>
            </Column>
            <Column header="" :style="{ width: '120px' }">
              <template #body="{ data: row }">
                <Button
                  class="p-button-danger p-button-outlined"
                  icon="pi pi-ban"
                  label="撤销"
                  :disabled="!!row.revoked_at"
                  @click="confirmRevoke(row)"
                />
              </template>
            </Column>
          </DataTable>
        </div>
      </template>
    </Card>

    <Dialog
      v-model:visible="newTokenDialogVisible"
      modal
      :draggable="false"
      :style="{ width: '760px', maxWidth: '94vw' }"
      header="新 Token（只显示一次）"
    >
      <div class="new-token">
        <div class="new-token-row">
          <span class="new-token-value">{{ newTokenValue }}</span>
          <Button class="p-button-outlined" icon="pi pi-copy" label="复制" @click="copyNewToken" />
        </div>
        <small class="muted">
          请立即保存；关闭后无法再次查看完整 Token（只能看到提示串）。
        </small>
      </div>

      <template #footer>
        <Button class="p-button-outlined" severity="secondary" label="关闭" @click="newTokenDialogVisible = false" />
      </template>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import Card from 'primevue/card'
import Button from 'primevue/button'
import DataTable from 'primevue/datatable'
import Column from 'primevue/column'
import Dialog from 'primevue/dialog'
import Tag from 'primevue/tag'
import InputText from 'primevue/inputtext'
import Dropdown from 'primevue/dropdown'
import Message from 'primevue/message'
import { useToast } from 'primevue/usetoast'
import { useConfirm } from 'primevue/useconfirm'
import { authApi } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'

type TokenRow = {
  id: string
  name: string
  token_hint: string
  expires_at?: string | null
  last_used_at?: string | null
  revoked_at?: string | null
  created_at: string
}

const authStore = useAuthStore()
const toast = useToast()
const confirm = useConfirm()

const isAdmin = computed(() => authStore.user?.role === 'admin')

const loading = ref(false)
const creating = ref(false)
const tokens = ref<TokenRow[]>([])

const tokenName = ref('')
const expiresInDays = ref<number>(30)
const expiresOptions = [
  { label: '1 天', value: 1 },
  { label: '7 天', value: 7 },
  { label: '30 天', value: 30 },
  { label: '90 天', value: 90 },
  { label: '180 天', value: 180 },
  { label: '365 天', value: 365 },
  { label: '不过期', value: 0 },
]

const newTokenDialogVisible = ref(false)
const newTokenValue = ref('')

const formatDateTime = (ts: string) => {
  if (!ts) return ''
  try {
    return new Date(ts).toLocaleString()
  } catch {
    return ts
  }
}

const isExpired = (expiresAt?: string | null) => {
  if (!expiresAt) return false
  const t = new Date(expiresAt).getTime()
  return Number.isFinite(t) && t <= Date.now()
}

const loadTokens = async () => {
  if (!isAdmin.value) return
  loading.value = true
  try {
    const res = await authApi.adminListApiTokens(100)
    if (res.data?.success && Array.isArray(res.data.data)) {
      tokens.value = res.data.data as TokenRow[]
    }
  } catch (e: any) {
    toast.add({ severity: 'error', summary: e.response?.data?.message || '获取 Token 列表失败', life: 3000 })
  } finally {
    loading.value = false
  }
}

const createToken = async () => {
  if (!isAdmin.value) return
  creating.value = true
  try {
    const res = await authApi.adminCreateApiToken(tokenName.value.trim() || undefined, expiresInDays.value)
    if (res.data?.success && res.data.data?.token) {
      newTokenValue.value = res.data.data.token
      newTokenDialogVisible.value = true
      tokenName.value = ''
      toast.add({ severity: 'success', summary: '已创建 Token', life: 1800 })
      await loadTokens()
    } else {
      toast.add({ severity: 'error', summary: res.data?.message || '创建 Token 失败', life: 3000 })
    }
  } catch (e: any) {
    toast.add({ severity: 'error', summary: e.response?.data?.message || '创建 Token 失败', life: 3000 })
  } finally {
    creating.value = false
  }
}

const copyNewToken = async () => {
  if (!newTokenValue.value) return
  try {
    await navigator.clipboard.writeText(newTokenValue.value)
    toast.add({ severity: 'success', summary: '已复制到剪贴板', life: 1600 })
  } catch {
    toast.add({ severity: 'warn', summary: '复制失败，请手动复制', life: 2500 })
  }
}

const confirmRevoke = (row: TokenRow) => {
  if (!isAdmin.value) return
  if (!row?.id || row.revoked_at) return

  confirm.require({
    message: `确认撤销 Token：${row.name || row.token_hint} ？`,
    header: '撤销确认',
    icon: 'pi pi-exclamation-triangle',
    acceptLabel: '撤销',
    rejectLabel: '取消',
    acceptClass: 'p-button-danger',
    accept: async () => {
      try {
        const res = await authApi.adminRevokeApiToken(row.id)
        if (res.data?.success) {
          toast.add({ severity: 'success', summary: '已撤销', life: 1800 })
          await loadTokens()
        } else {
          toast.add({ severity: 'error', summary: res.data?.message || '撤销失败', life: 3000 })
        }
      } catch (e: any) {
        toast.add({ severity: 'error', summary: e.response?.data?.message || '撤销失败', life: 3000 })
      }
    },
  })
}

onMounted(() => {
  loadTokens()
})
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.name-input {
  min-width: 180px;
}

.expires-dropdown {
  min-width: 140px;
}

.content {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.muted {
  color: var(--p-text-muted-color);
}

.tokens-table :deep(.p-datatable-table) {
  width: 100%;
  table-layout: fixed;
}

.tokens-table :deep(.p-datatable-thead > tr > th),
.tokens-table :deep(.p-datatable-tbody > tr > td) {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.new-token {
  border: 1px dashed color-mix(in srgb, var(--p-surface-200), transparent 15%);
  border-radius: var(--radius-md);
  padding: 12px;
  background: color-mix(in srgb, var(--p-surface-0), transparent 0%);
}

.new-token-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
  margin-bottom: 6px;
}

.new-token-value {
  font-family: var(--font-mono);
  font-weight: 800;
  letter-spacing: 0.2px;
  word-break: break-all;
}
</style>

