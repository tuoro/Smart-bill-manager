<template>
  <div class="page">
    <Card class="panel sbm-surface">
      <template #title>
        <div class="panel-title">
          <span>用户</span>
          <Button :label="'刷新'" icon="pi pi-refresh" class="p-button-outlined" @click="loadUsers" />
        </div>
      </template>
      <template #content>
        <DataTable
          class="sbm-dt-fixed"
          :value="users"
          :loading="loading"
          :paginator="true"
          :rows="pageSize"
          :rowsPerPageOptions="[10, 20, 50, 100]"
          :tableStyle="{ minWidth: '920px', tableLayout: 'fixed' }"
          responsiveLayout="scroll"
          @page="onPage"
        >
          <Column field="username" header="用户名" :style="{ width: '180px' }">
            <template #body="{ data: row }">
              <span class="sbm-ellipsis" :title="row.username">{{ row.username }}</span>
            </template>
          </Column>
          <Column field="role" header="角色" :style="{ width: '120px' }">
            <template #body="{ data: row }">
              <Tag :severity="row.role === 'admin' ? 'danger' : 'secondary'" :value="row.role" />
            </template>
          </Column>
          <Column field="is_active" header="状态" :style="{ width: '120px' }">
            <template #body="{ data: row }">
              <Tag :severity="row.is_active ? 'success' : 'secondary'" :value="row.is_active ? '启用' : '停用'" />
            </template>
          </Column>
          <Column header="管理" :style="{ width: '200px' }">
            <template #body="{ data: row }">
              <div class="actions">
                <Button
                  v-if="row.is_active"
                  size="small"
                  class="p-button-outlined"
                  severity="warning"
                  icon="pi pi-ban"
                  :label="'停用'"
                  :disabled="isSelf(row.id)"
                  :loading="workingId === row.id"
                  @click="confirmToggleActive(row, false)"
                />
                <Button
                  v-else
                  size="small"
                  class="p-button-outlined"
                  severity="success"
                  icon="pi pi-check"
                  :label="'启用'"
                  :disabled="isSelf(row.id)"
                  :loading="workingId === row.id"
                  @click="confirmToggleActive(row, true)"
                />

                <Button
                  size="small"
                  class="p-button-text"
                  severity="danger"
                  icon="pi pi-trash"
                  :label="'删除'"
                  :disabled="isSelf(row.id)"
                  :loading="deletingId === row.id"
                  @click="confirmDeleteUser(row)"
                />
              </div>
            </template>
          </Column>
          <Column header="代操作" :style="{ width: '140px' }">
            <template #body="{ data: row }">
              <Button
                v-if="currentActAsUserId !== row.id"
                class="p-button-outlined"
                severity="secondary"
                size="small"
                icon="pi pi-user-edit"
                :label="'代操作'"
                @click="startActAs(row)"
              />
              <Button
                v-else
                class="p-button-outlined"
                severity="danger"
                size="small"
                icon="pi pi-times"
                :label="'退出'"
                @click="stopActAs"
              />
            </template>
          </Column>
          <Column header="ID" :style="{ width: '360px' }">
            <template #body="{ data: row }">
              <span class="mono sbm-ellipsis" :title="row.id">{{ row.id }}</span>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import Card from 'primevue/card'
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { useConfirm } from 'primevue/useconfirm'
import { useToast } from 'primevue/usetoast'
import { authApi } from '@/api/auth'
import { clearActAs, getActAsUserId, setActAsUser } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { isRequestCanceled } from '@/utils/http'
import type { User } from '@/types'

const toast = useToast()
const confirm = useConfirm()
const authStore = useAuthStore()
const actorUserId = computed(() => authStore.user?.id || null)
const users = ref<User[]>([])
const loading = ref(false)
const currentActAsUserId = ref<string | null>(null)
const pageSize = ref(10)
const usersAbort = ref<AbortController | null>(null)
const workingId = ref<string | null>(null)
const deletingId = ref<string | null>(null)

const onPage = (e: any) => {
  pageSize.value = e?.rows || pageSize.value
}

const refreshActAsState = () => {
  currentActAsUserId.value = getActAsUserId()
}

const startActAs = (user: User) => {
  setActAsUser(user.id, user.username)
  refreshActAsState()
  toast.add({ severity: 'info', summary: `已进入代操作：${user.username}`, life: 2500 })
}

const stopActAs = () => {
  clearActAs()
  refreshActAsState()
  toast.add({ severity: 'success', summary: '已退出代操作', life: 2000 })
}

const isSelf = (id: string) => {
  const actor = actorUserId.value
  return !!actor && actor === id
}

const toggleActive = async (user: User, active: boolean) => {
  if (workingId.value) return
  workingId.value = user.id
  try {
    const res = await authApi.adminSetUserActive(user.id, active)
    if (res.data.success && res.data.data) {
      const next = users.value.map((u) => (u.id === user.id ? res.data.data! : u))
      users.value = next
      toast.add({
        severity: 'success',
        summary: active ? '已启用用户' : '已停用用户',
        life: 2000,
      })
      return
    }
    toast.add({ severity: 'error', summary: res.data.message || '操作失败', life: 3000 })
  } catch (e: any) {
    if (isRequestCanceled(e)) return
    toast.add({ severity: 'error', summary: e.response?.data?.message || '操作失败', life: 3000 })
  } finally {
    workingId.value = null
  }
}

const confirmToggleActive = (user: User, active: boolean) => {
  if (isSelf(user.id)) {
    toast.add({ severity: 'warn', summary: '不能对自己执行该操作', life: 2500 })
    return
  }
  confirm.require({
    header: active ? '启用用户' : '停用用户',
    message: active ? `确认启用用户：${user.username}？` : `确认停用用户：${user.username}？`,
    icon: 'pi pi-exclamation-triangle',
    acceptLabel: '确认',
    rejectLabel: '取消',
    accept: () => void toggleActive(user, active),
  })
}

const deleteUser = async (user: User) => {
  if (deletingId.value) return
  deletingId.value = user.id
  try {
    const res = await authApi.adminDeleteUser(user.id)
    if (res.data.success) {
      users.value = users.value.filter((u) => u.id !== user.id)
      if (currentActAsUserId.value === user.id) {
        clearActAs()
        refreshActAsState()
      }
      toast.add({ severity: 'success', summary: '用户已删除', life: 2000 })
      return
    }
    toast.add({ severity: 'error', summary: res.data.message || '删除失败', life: 3000 })
  } catch (e: any) {
    if (isRequestCanceled(e)) return
    toast.add({ severity: 'error', summary: e.response?.data?.message || '删除失败', life: 3000 })
  } finally {
    deletingId.value = null
  }
}

const confirmDeleteUser = (user: User) => {
  if (isSelf(user.id)) {
    toast.add({ severity: 'warn', summary: '不能删除自己的账号', life: 2500 })
    return
  }
  confirm.require({
    header: '删除用户',
    message: `确认删除用户：${user.username}？该用户的数据将被删除。`,
    icon: 'pi pi-exclamation-triangle',
    acceptLabel: '删除',
    rejectLabel: '取消',
    acceptClass: 'p-button-danger',
    accept: () => void deleteUser(user),
  })
}

const loadUsers = async () => {
  usersAbort.value?.abort()
  const controller = new AbortController()
  usersAbort.value = controller
  loading.value = true
  try {
    const res = await authApi.adminListUsers({ signal: controller.signal })
    if (res.data.success && res.data.data) users.value = res.data.data
  } catch (e: any) {
    if (isRequestCanceled(e)) return
    toast.add({ severity: 'error', summary: '加载用户失败', life: 3000 })
  } finally {
    loading.value = false
    if (usersAbort.value === controller) usersAbort.value = null
  }
}

onMounted(loadUsers)
onMounted(() => {
  refreshActAsState()
  if (typeof window !== 'undefined') window.addEventListener('sbm-act-as-change', refreshActAsState)
})

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') window.removeEventListener('sbm-act-as-change', refreshActAsState)
  usersAbort.value?.abort()
})
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.panel-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace;
  font-size: 12px;
  color: var(--p-text-muted-color);
}

.sbm-ellipsis {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
