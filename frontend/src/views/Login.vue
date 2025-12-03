<template>
  <div class="login-container">
    <el-card class="login-card">
      <div class="login-header">
        <h2 class="title">💰 智能账单管理</h2>
        <p class="subtitle">Smart Bill Manager</p>
      </div>
      
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
        size="large"
        @submit.prevent="handleLogin"
      >
        <el-form-item prop="username">
          <el-input
            v-model="form.username"
            placeholder="用户名"
            :prefix-icon="User"
            autocomplete="username"
          />
        </el-form-item>
        
        <el-form-item prop="password">
          <el-input
            v-model="form.password"
            type="password"
            placeholder="密码"
            :prefix-icon="Lock"
            autocomplete="current-password"
            show-password
          />
        </el-form-item>
        
        <el-form-item>
          <el-button
            type="primary"
            :loading="loading"
            class="login-button"
            native-type="submit"
          >
            登录
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { User, Lock } from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const authStore = useAuthStore()

const formRef = ref<FormInstance>()
const loading = ref(false)

const form = reactive({
  username: '',
  password: ''
})

const rules: FormRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' }
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' }
  ]
}

const handleLogin = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    
    loading.value = true
    const result = await authStore.login(form.username, form.password)
    loading.value = false
    
    if (result.success) {
      ElMessage.success('登录成功')
      router.push('/dashboard')
    } else {
      ElMessage.error(result.message)
    }
  })
}
</script>

<style scoped>
.login-container {
  min-height: 100vh;
  display: flex;
  justify-content: center;
  align-items: center;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  position: relative;
  overflow: hidden;
}

/* Animated background */
.login-container::before {
  content: '';
  position: absolute;
  width: 200%;
  height: 200%;
  top: -50%;
  left: -50%;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.1) 1px, transparent 1px);
  background-size: 50px 50px;
  animation: moveBackground 20s linear infinite;
}

@keyframes moveBackground {
  0% {
    transform: translate(0, 0);
  }
  100% {
    transform: translate(50px, 50px);
  }
}

/* Floating orbs */
.login-container::after {
  content: '';
  position: absolute;
  width: 300px;
  height: 300px;
  background: radial-gradient(circle, rgba(255, 255, 255, 0.1), transparent);
  border-radius: 50%;
  top: 10%;
  right: 10%;
  animation: float 6s ease-in-out infinite;
  pointer-events: none;
}

.login-card {
  width: 420px;
  max-width: 90vw;
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-xl);
  background: rgba(255, 255, 255, 0.95);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border: 1px solid rgba(255, 255, 255, 0.3);
  position: relative;
  z-index: 1;
  animation: scaleIn 0.5s ease;
  padding: 32px 24px;
}

.login-card :deep(.el-card__body) {
  padding: 0;
}

.login-header {
  text-align: center;
  margin-bottom: 32px;
}

.title {
  margin: 0;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-size: 28px;
  font-weight: 700;
  letter-spacing: -0.5px;
}

.subtitle {
  margin: 12px 0 0;
  color: var(--color-text-tertiary);
  font-size: 14px;
  font-weight: 500;
  text-transform: uppercase;
  letter-spacing: 1px;
}

.login-card :deep(.el-form-item) {
  margin-bottom: 24px;
}

.login-card :deep(.el-input__wrapper) {
  border-radius: var(--radius-md);
  padding: 12px 16px;
  box-shadow: var(--shadow-sm);
  transition: all var(--transition-base);
  border: 1px solid rgba(0, 0, 0, 0.06);
}

.login-card :deep(.el-input__wrapper:hover) {
  border-color: rgba(102, 126, 234, 0.3);
  box-shadow: 0 2px 12px rgba(102, 126, 234, 0.15);
}

.login-card :deep(.el-input__wrapper.is-focus) {
  border-color: #667eea;
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
}

.login-card :deep(.el-input__inner) {
  font-size: 15px;
}

.login-button {
  width: 100%;
  height: 48px;
  font-size: 16px;
  font-weight: 600;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  border: none;
  border-radius: var(--radius-md);
  box-shadow: 0 4px 14px rgba(102, 126, 234, 0.4);
  transition: all var(--transition-base);
  position: relative;
  overflow: hidden;
}

.login-button::before {
  content: '';
  position: absolute;
  top: 0;
  left: -100%;
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.3), transparent);
  transition: left 0.5s;
}

.login-button:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(102, 126, 234, 0.5);
  background: linear-gradient(135deg, #5a6fd6 0%, #6a4291 100%);
}

.login-button:hover::before {
  left: 100%;
}

.login-button:active {
  transform: translateY(0);
  box-shadow: 0 2px 8px rgba(102, 126, 234, 0.3);
}

@media (max-width: 480px) {
  .login-card {
    padding: 24px 16px;
  }
  
  .title {
    font-size: 24px;
  }
}
</style>
