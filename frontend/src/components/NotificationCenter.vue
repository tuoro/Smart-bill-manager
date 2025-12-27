<template>
  <div class="nc">
    <Button
      class="nc-btn"
      severity="secondary"
      text
      icon="pi pi-bell"
      aria-label="消息"
      @click="toggle"
    />
    <span v-if="unreadCount > 0" class="nc-badge" aria-hidden="true">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>

    <OverlayPanel ref="panel" :dismissable="true" :showCloseIcon="false" class="nc-panel">
      <div class="nc-header">
        <div class="nc-title">消息</div>
        <div class="nc-actions">
          <Button class="p-button-text" size="small" :label="'全部已读'" @click="markAllRead" />
          <Button class="p-button-text p-button-danger" size="small" :label="'清空'" @click="clear" />
        </div>
      </div>

      <div v-if="items.length === 0" class="nc-empty">暂无消息</div>

      <div v-else class="nc-list">
        <button
          v-for="n in items"
          :key="n.id"
          type="button"
          class="nc-item"
          :class="{ unread: !n.read }"
          @click="handleItemClick(n.id)"
        >
          <div class="nc-row">
            <span class="nc-dot" aria-hidden="true" />
            <div class="nc-main">
              <div class="nc-top">
                <span class="nc-text" :title="n.title">{{ n.title }}</span>
                <Tag :severity="severityToTag(n.severity)" class="nc-tag" :value="severityLabel(n.severity)" />
              </div>
              <div v-if="n.detail" class="nc-detail" :title="n.detail">{{ n.detail }}</div>
              <div class="nc-time">{{ formatTime(n.createdAt) }}</div>
            </div>
          </div>
        </button>
      </div>
    </OverlayPanel>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import Button from 'primevue/button'
import OverlayPanel from 'primevue/overlaypanel'
import Tag from 'primevue/tag'
import { useNotificationStore } from '@/stores/notifications'
import type { NotificationSeverity } from '@/stores/notifications'

const store = useNotificationStore()
const panel = ref<InstanceType<typeof OverlayPanel> | null>(null)

const items = computed(() => store.items)
const unreadCount = computed(() => store.unreadCount)

const toggle = (event: MouseEvent) => {
  panel.value?.toggle(event)
}

const realign = async () => {
  await nextTick()
  const p = panel.value
  if (!p) return
  if (typeof window !== 'undefined' && typeof window.requestAnimationFrame === 'function') {
    window.requestAnimationFrame(() => p.alignOverlay())
    return
  }
  p.alignOverlay()
}

const handleItemClick = async (id: string) => {
  store.markRead(id)
  await realign()
}

const markAllRead = async () => {
  store.markAllRead()
  await realign()
}

const clear = async () => {
  store.clear()
  await realign()
}

const formatTime = (ts: number) => {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString()
}

const severityLabel = (s: NotificationSeverity) => {
  if (s === 'success') return '成功'
  if (s === 'warn') return '提示'
  if (s === 'error') return '错误'
  return '信息'
}

const severityToTag = (s: NotificationSeverity): 'success' | 'info' | 'warn' | 'danger' => {
  if (s === 'error') return 'danger'
  return s
}
</script>

<style scoped>
.nc {
  position: relative;
}

.nc-btn {
  width: 42px;
  height: 42px;
  border-radius: 12px !important;
}

.nc-badge {
  position: absolute;
  top: -2px;
  right: -2px;
  min-width: 18px;
  height: 18px;
  padding: 0 5px;
  border-radius: 999px;
  background: var(--p-red-500, #ef4444);
  color: white;
  font-size: 11px;
  font-weight: 800;
  display: grid;
  place-items: center;
  border: 2px solid var(--p-surface-0);
}

:deep(.nc-panel) {
  width: min(460px, 92vw);
}

.nc-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 2px 4px 10px;
  border-bottom: 1px solid rgba(2, 6, 23, 0.06);
}

.nc-title {
  font-size: 14px;
  font-weight: 800;
  color: var(--p-text-color);
}

.nc-actions {
  display: flex;
  gap: 6px;
}

.nc-empty {
  padding: 14px 6px 6px;
  color: var(--p-text-muted-color);
  font-weight: 700;
}

.nc-list {
  margin-top: 10px;
  display: flex;
  flex-direction: column;
  max-height: min(420px, 56vh);
  overflow: auto;
}

.nc-item {
  width: 100%;
  text-align: left;
  border: 0;
  background: transparent;
  padding: 10px 6px;
  border-radius: 12px;
  cursor: pointer;
}

.nc-item:hover {
  background: rgba(2, 6, 23, 0.04);
}

.nc-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.nc-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: transparent;
  margin-top: 6px;
}

.nc-item.unread .nc-dot {
  background: var(--p-primary-500, #3b82f6);
}

.nc-main {
  min-width: 0;
  flex: 1;
}

.nc-top {
  display: flex;
  align-items: center;
  gap: 8px;
}

.nc-text {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 800;
  color: var(--p-text-color);
}

.nc-tag {
  flex: 0 0 auto;
}

.nc-detail {
  margin-top: 4px;
  color: var(--p-text-muted-color);
  font-weight: 650;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.nc-time {
  margin-top: 6px;
  font-size: 12px;
  color: var(--p-text-muted-color);
}
</style>
