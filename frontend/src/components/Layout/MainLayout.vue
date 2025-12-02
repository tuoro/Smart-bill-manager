<template>
  <el-container class="layout-container">
    <el-aside :width="isCollapsed ? '64px' : '200px'" class="sidebar">
      <div class="logo">
        {{ isCollapsed ? '💰' : '💰 智能账单管理' }}
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
          <template #title>仪表盘</template>
        </el-menu-item>
        <el-menu-item index="/payments">
          <el-icon><Wallet /></el-icon>
          <template #title>支付记录</template>
        </el-menu-item>
        <el-menu-item index="/invoices">
          <el-icon><Document /></el-icon>
          <template #title>发票管理</template>
        </el-menu-item>
        <el-menu-item index="/email">
          <el-icon><Message /></el-icon>
          <template #title>邮箱监控</template>
        </el-menu-item>
        <el-menu-item index="/dingtalk">
          <el-icon><ChatDotRound /></el-icon>
          <template #title>钉钉机器人</template>
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
                  {{ authStore.user?.username || '用户' }}
                </el-dropdown-item>
                <el-dropdown-item command="change-password">
                  <el-icon><Key /></el-icon>
                  修改密码
                </el-dropdown-item>
                <el-dropdown-item divided command="logout">
                  <el-icon><SwitchButton /></el-icon>
                  退出登录
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
  </el-container>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { 
  Odometer, Wallet, Document, Message, ChatDotRound,
  User, Key, SwitchButton, Expand, Fold
} from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()

const isCollapsed = ref(false)

const currentRoute = computed(() => route.path)

const pageTitle = computed(() => {
  const titles: Record<string, string> = {
    '/dashboard': '仪表盘',
    '/payments': '支付记录',
    '/invoices': '发票管理',
    '/email': '邮箱监控',
    '/dingtalk': '钉钉机器人'
  }
  return titles[route.path] || '仪表盘'
})

const handleMenuSelect = (index: string) => {
  router.push(index)
}

const handleUserCommand = (command: string) => {
  if (command === 'logout') {
    authStore.logout()
    ElMessage.success('已退出登录')
    router.push('/login')
  } else if (command === 'change-password') {
    ElMessage.info('修改密码功能开发中...')
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
}

.sidebar-menu {
  flex: 1;
  border-right: none;
}

.sidebar-menu:not(.el-menu--collapse) {
  width: 200px;
}

:deep(.el-menu-item.is-active) {
  background: linear-gradient(90deg, #1890ff 0%, transparent 100%) !important;
}

.collapse-trigger {
  height: 48px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.2);
  color: rgba(255, 255, 255, 0.85);
  cursor: pointer;
}

.collapse-trigger:hover {
  color: #1890ff;
}

.header {
  background: #fff;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  box-shadow: 0 1px 4px rgba(0,21,41,.08);
}

.page-title {
  margin: 0;
  color: #1890ff;
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
}

.username {
  color: #333;
}

.main-content {
  background: #f0f2f5;
  padding: 24px;
  min-height: 280px;
  border-radius: 8px;
}
</style>
