<template>
  <el-container class="layout-container">
    <el-aside :width="isCollapsed ? '64px' : '200px'" class="sidebar">
      <div class="logo">
        {{ isCollapsed ? '\uD83D\uDCB0' : '\uD83D\uDCB0 \u667A\u80FD\u8D26\u5355\u7BA1\u7406' }}
      </div>
      <el-menu
        :default-active="currentRoute"
        class="sidebar-menu"
        :collapse="isCollapsed"
        :collapse-transition="false"
        background-color="transparent"
        text-color="rgba(255,255,255,0.85)"
        active-text-color="#1890ff"
        @select="handleMenuSelect"
      >
        <el-menu-item index="/dashboard">
          <el-icon><Odometer /></el-icon>
          <template #title>&#x4EEA;&#x8868;&#x76D8;</template>
        </el-menu-item>
        <el-menu-item index="/payments">
          <el-icon><Wallet /></el-icon>
          <template #title>&#x652F;&#x4ED8;&#x8BB0;&#x5F55;</template>
        </el-menu-item>
        <el-menu-item index="/invoices">
          <el-icon><Document /></el-icon>
          <template #title>&#x53D1;&#x7968;&#x7BA1;&#x7406;</template>
        </el-menu-item>
        <el-menu-item index="/email">
          <el-icon><Message /></el-icon>
          <template #title>&#x90AE;&#x7BB1;&#x76D1;&#x63A7;</template>
        </el-menu-item>
        <el-menu-item index="/dingtalk">
          <el-icon><ChatDotRound /></el-icon>
          <template #title>&#x9489;&#x9489;&#x673A;&#x5668;&#x4EBA;</template>
        </el-menu-item>
        <el-menu-item index="/logs">
          <el-icon><Document /></el-icon>
          <template #title>&#x65E5;&#x5FD7;</template>
        </el-menu-item>
      </el-menu>
      <div class="collapse-trigger" @click="isCollapsed = !isCollapsed">
        <el-icon v-if="isCollapsed"><Expand /></el-icon>
        <el-icon v-else><Fold /></el-icon>
      </div>
    </el-aside>

    <el-container>
      <el-header class="header">
        <h2 class="page-title">{{ pageTitle }}</h2>
        <div class="header-right">
          <el-dropdown @command="handleUserCommand">
            <div class="user-info">
              <el-avatar :size="32" :icon="User" />
              <span class="username">{{ authStore.user?.username }}</span>
            </div>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item disabled>
                  <el-icon><User /></el-icon>
                  {{ authStore.user?.username || '\u7528\u6237' }}
                </el-dropdown-item>
                <el-dropdown-item command="change-password">
                  <el-icon><Key /></el-icon>
                  &#x4FEE;&#x6539;&#x5BC6;&#x7801;
                </el-dropdown-item>
                <el-dropdown-item divided command="logout">
                  <el-icon><SwitchButton /></el-icon>
                  &#x9000;&#x51FA;&#x767B;&#x5F55;
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>

      <el-main class="main-content">
        <router-view />
      </el-main>
    </el-container>

    <ChangePassword v-model="showChangePasswordDialog" />
  </el-container>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ChatDotRound, Document, Expand, Fold, Key, Message, Odometer, SwitchButton, User, Wallet } from '@element-plus/icons-vue'
import ChangePassword from '@/components/ChangePassword.vue'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const isCollapsed = ref(false)
const showChangePasswordDialog = ref(false)

const currentRoute = computed(() => route.path)

const pageTitle = computed(() => {
  const titles: Record<string, string> = {
    '/dashboard': '\u4EEA\u8868\u76D8',
    '/payments': '\u652F\u4ED8\u8BB0\u5F55',
    '/invoices': '\u53D1\u7968\u7BA1\u7406',
    '/email': '\u90AE\u7BB1\u76D1\u63A7',
    '/dingtalk': '\u9489\u9489\u673A\u5668\u4EBA',
    '/logs': '\u65E5\u5FD7',
  }
  return titles[route.path] || titles['/dashboard']
})

const handleMenuSelect = (index: string) => {
  router.push(index)
}

const handleUserCommand = (command: string) => {
  if (command === 'logout') {
    authStore.logout()
    ElMessage.success('\u5DF2\u9000\u51FA\u767B\u5F55')
    router.push('/login')
  } else if (command === 'change-password') {
    showChangePasswordDialog.value = true
  }
}
</script>

<style scoped>
.layout-container {
  min-height: 100vh;
}

.sidebar {
  background: linear-gradient(180deg, #001529 0%, #003a70 100%);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  transition: width var(--transition-base);
  position: relative;
}

/* Glassmorphism overlay for sidebar */
.sidebar::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.05) 0%, transparent 100%);
  pointer-events: none;
}

.logo {
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  font-size: 18px;
  font-weight: bold;
  border-bottom: 1px solid rgba(255,255,255,0.1);
  white-space: nowrap;
  overflow: hidden;
  transition: all var(--transition-base);
  position: relative;
  z-index: 1;
}

.sidebar-menu {
  flex: 1;
  border-right: none;
  padding: 8px 0;
}

.sidebar-menu:not(.el-menu--collapse) {
  width: 200px;
}

:deep(.el-menu-item) {
  margin: 4px 8px;
  border-radius: var(--radius-md);
  transition: all var(--transition-base);
  position: relative;
  overflow: hidden;
}

:deep(.el-menu-item::before) {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  background: #1890ff;
  transform: scaleY(0);
  transition: transform var(--transition-base);
}

:deep(.el-menu-item:hover) {
  background: rgba(255, 255, 255, 0.08) !important;
  transform: translateX(2px);
}

:deep(.el-menu-item.is-active) {
  background: rgba(24, 144, 255, 0.15) !important;
  box-shadow: 0 2px 8px rgba(24, 144, 255, 0.2);
}

:deep(.el-menu-item.is-active::before) {
  transform: scaleY(1);
}

:deep(.el-menu-item .el-icon) {
  transition: all var(--transition-base);
}

:deep(.el-menu-item:hover .el-icon),
:deep(.el-menu-item.is-active .el-icon) {
  transform: scale(1.1);
}

.collapse-trigger {
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.2);
  color: rgba(255, 255, 255, 0.85);
  cursor: pointer;
  transition: all var(--transition-base);
  position: relative;
  z-index: 1;
}

.collapse-trigger:hover {
  color: #1890ff;
  background: rgba(24, 144, 255, 0.1);
}

.collapse-trigger .el-icon {
  transition: transform var(--transition-base);
}

.collapse-trigger:hover .el-icon {
  transform: scale(1.2);
}

.header {
  background: #fff;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
  position: relative;
  z-index: 10;
}

.page-title {
  margin: 0;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-size: 20px;
  font-weight: 600;
  animation: slideInLeft 0.3s ease;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

.user-info {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  padding: 8px 12px;
  border-radius: var(--radius-md);
  transition: all var(--transition-base);
}

.user-info:hover {
  background: rgba(0, 0, 0, 0.04);
}

.user-info :deep(.el-avatar) {
  transition: all var(--transition-base);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

.user-info:hover :deep(.el-avatar) {
  transform: scale(1.05);
  box-shadow: 0 4px 12px rgba(102, 126, 234, 0.3);
}

.username {
  color: var(--color-text-primary);
  font-weight: 500;
  transition: color var(--transition-base);
}

.user-info:hover .username {
  color: var(--color-primary);
}

.main-content {
  background: var(--color-bg-primary);
  padding: 24px;
  min-height: 280px;
  overflow-y: auto;
  animation: fadeIn 0.3s ease;
}

/* Page transition */
.main-content > * {
  animation: fadeIn 0.4s ease;
}

/* Dropdown menu enhancements */
:deep(.el-dropdown-menu__item) {
  transition: all var(--transition-base);
  border-radius: var(--radius-sm);
  margin: 4px 8px;
}

:deep(.el-dropdown-menu__item:hover) {
  background: rgba(102, 126, 234, 0.1);
  transform: translateX(2px);
}

:deep(.el-dropdown-menu__item .el-icon) {
  transition: all var(--transition-base);
}

:deep(.el-dropdown-menu__item:hover .el-icon) {
  transform: scale(1.1);
}

@media (max-width: 768px) {
  .header {
    padding: 0 16px;
  }

  .page-title {
    font-size: 18px;
  }

  .username {
    display: none;
  }
}
</style>

