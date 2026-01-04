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
        <DataTable :value="users" :loading="loading" :paginator="true" :rows="10" responsiveLayout="scroll">
          <Column field="username" header="用户名" />
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
          <Column header="ID" :style="{ width: '320px' }">
            <template #body="{ data: row }">
              <span class="mono">{{ row.id }}</span>
            </template>
          </Column>
        </DataTable>
      </template>
    </Card>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import Card from 'primevue/card'
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tag from 'primevue/tag'
import { useToast } from 'primevue/usetoast'
import api from '@/api/auth'
import { clearActAs, getActAsUserId, setActAsUser } from '@/api'
import type { ApiResponse, User } from '@/types'

const toast = useToast()
const users = ref<User[]>([])
const loading = ref(false)
const currentActAsUserId = ref<string | null>(null)

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

const loadUsers = async () => {
  loading.value = true
  try {
    const res = await api.get<ApiResponse<User[]>>('/admin/users')
    if (res.data.success && res.data.data) users.value = res.data.data
  } catch {
    toast.add({ severity: 'error', summary: '加载用户失败', life: 3000 })
  } finally {
    loading.value = false
  }
}

onMounted(loadUsers)
onMounted(() => {
  refreshActAsState()
  if (typeof window !== 'undefined') window.addEventListener('sbm-act-as-change', refreshActAsState)
})

onBeforeUnmount(() => {
  if (typeof window !== 'undefined') window.removeEventListener('sbm-act-as-change', refreshActAsState)
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
</style>
