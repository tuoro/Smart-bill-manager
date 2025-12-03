<template>
  <div class="setup-container">
    <el-card class="setup-card">
      <div class="setup-header">
        <h2 class="title">💰 初始化设置</h2>
        <p class="subtitle">欢迎使用智能账单管理系统</p>
        <p class="description">请创建管理员账户以开始使用</p>
      </div>
      
      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        label-position="top"
        size="large"
        @submit.prevent="handleSetup"
      >
        <el-form-item label="用户名" prop="username">
          <el-input
            v-model="form.username"
            placeholder="请输入管理员用户名 (3-50字符)"
            :prefix-icon="User"
            autocomplete="username"
          />
        </el-form-item>
        
        <el-form-item label="密码" prop="password">
          <el-input
            v-model="form.password"
            type="password"
            placeholder="请输入密码 (至少6位)"
            :prefix-icon="Lock"
            autocomplete="new-password"
            show-password
            @input="updatePasswordStrength"
          />
          <div v-if="form.password" class="password-strength">
            <span :class="['strength-indicator', passwordStrength.level]">
              密码强度: {{ passwordStrength.text }}
            </span>
          </div>
        </el-form-item>
        
        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input
            v-model="form.confirmPassword"
            type="password"
            placeholder="请再次输入密码"
            :prefix-icon="Lock"
            autocomplete="new-password"
            show-password
          />
        </el-form-item>
        
        <el-form-item label="邮箱 (可选)" prop="email">
          <el-input
            v-model="form.email"
            placeholder="请输入邮箱地址"
            :prefix-icon="Message"
            autocomplete="email"
          />
        </el-form-item>
        
        <el-form-item>
          <el-button
            type="primary"
            :loading="loading"
            class="setup-button"
            native-type="submit"
          >
            创建管理员账户
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
import { User, Lock, Message } from '@element-plus/icons-vue'
import { authApi, setToken, setStoredUser } from '@/api/auth'
import { checkPasswordStrength, type PasswordStrength } from '@/utils/password'

const router = useRouter()

const formRef = ref<FormInstance>()
const loading = ref(false)

const form = reactive({
  username: '',
  password: '',
  confirmPassword: '',
  email: ''
})

const passwordStrength = ref<PasswordStrength>({
  level: 'weak',
  text: '弱'
})

const updatePasswordStrength = () => {
  passwordStrength.value = checkPasswordStrength(form.password)
}

const validatePassword = (_rule: any, value: string, callback: any) => {
  if (value === '') {
    callback(new Error('请输入密码'))
  } else if (value.length < 6) {
    callback(new Error('密码长度至少6个字符'))
  } else {
    callback()
  }
}

const validateConfirmPassword = (_rule: any, value: string, callback: any) => {
  if (value === '') {
    callback(new Error('请再次输入密码'))
  } else if (value !== form.password) {
    callback(new Error('两次输入密码不一致'))
  } else {
    callback()
  }
}

const validateEmail = (_rule: any, value: string, callback: any) => {
  if (value && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) {
    callback(new Error('请输入有效的邮箱地址'))
  } else {
    callback()
  }
}

const rules: FormRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { min: 3, max: 50, message: '用户名长度应为3-50个字符', trigger: 'blur' }
  ],
  password: [
    { required: true, validator: validatePassword, trigger: 'blur' }
  ],
  confirmPassword: [
    { required: true, validator: validateConfirmPassword, trigger: 'blur' }
  ],
  email: [
    { validator: validateEmail, trigger: 'blur' }
  ]
}

const handleSetup = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    
    loading.value = true
    try {
      const response = await authApi.setup(
        form.username,
        form.password,
        form.email || undefined
      )
      
      if (response.data.success) {
        // Save token and user
        if (response.data.token) {
          setToken(response.data.token)
        }
        if (response.data.user) {
          setStoredUser(response.data.user)
        }
        
        ElMessage.success('管理员账户创建成功！')
        setTimeout(() => {
          router.push('/dashboard')
        }, 500)
      } else {
        ElMessage.error(response.data.message || '创建失败')
      }
    } catch (error: any) {
      console.error('Setup error:', error)
      ElMessage.error(error.response?.data?.message || '创建失败，请稍后重试')
    } finally {
      loading.value = false
    }
  })
}
</script>

<style scoped>
.setup-container {
  min-height: 100vh;
  display: flex;
  justify-content: center;
  align-items: center;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  position: relative;
  overflow: hidden;
}

/* Animated background */
.setup-container::before {
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
.setup-container::after {
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

.setup-card {
  width: 500px;
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

.setup-card :deep(.el-card__body) {
  padding: 0;
}

.setup-header {
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
  color: var(--color-text-primary);
  font-size: 16px;
  font-weight: 600;
}

.description {
  margin: 8px 0 0;
  color: var(--color-text-tertiary);
  font-size: 14px;
}

.setup-card :deep(.el-form-item) {
  margin-bottom: 24px;
}

.setup-card :deep(.el-input__wrapper) {
  border-radius: var(--radius-md);
  padding: 12px 16px;
  box-shadow: var(--shadow-sm);
  transition: all var(--transition-base);
  border: 1px solid rgba(0, 0, 0, 0.06);
}

.setup-card :deep(.el-input__wrapper:hover) {
  border-color: rgba(102, 126, 234, 0.3);
  box-shadow: 0 2px 12px rgba(102, 126, 234, 0.15);
}

.setup-card :deep(.el-input__wrapper.is-focus) {
  border-color: #667eea;
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
}

.setup-card :deep(.el-input__inner) {
  font-size: 15px;
}

.setup-card :deep(.el-form-item__label) {
  font-weight: 500;
  color: var(--color-text-primary);
}

.password-strength {
  margin-top: 8px;
}

.strength-indicator {
  font-size: 12px;
  padding: 4px 10px;
  border-radius: var(--radius-sm);
  font-weight: 500;
  transition: all var(--transition-base);
  display: inline-block;
}

.strength-indicator.weak {
  color: #f56c6c;
  background: linear-gradient(135deg, rgba(245, 108, 108, 0.15), rgba(245, 86, 108, 0.15));
}

.strength-indicator.medium {
  color: #e6a23c;
  background: linear-gradient(135deg, rgba(230, 162, 60, 0.15), rgba(254, 225, 64, 0.15));
}

.strength-indicator.strong {
  color: #67c23a;
  background: linear-gradient(135deg, rgba(103, 194, 58, 0.15), rgba(56, 249, 215, 0.15));
}

.setup-button {
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

.setup-button::before {
  content: '';
  position: absolute;
  top: 0;
  left: -100%;
  width: 100%;
  height: 100%;
  background: linear-gradient(90deg, transparent, rgba(255, 255, 255, 0.3), transparent);
  transition: left 0.5s;
}

.setup-button:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(102, 126, 234, 0.5);
  background: linear-gradient(135deg, #5a6fd6 0%, #6a4291 100%);
}

.setup-button:hover::before {
  left: 100%;
}

.setup-button:active {
  transform: translateY(0);
  box-shadow: 0 2px 8px rgba(102, 126, 234, 0.3);
}

@media (max-width: 480px) {
  .setup-card {
    padding: 24px 16px;
  }
  
  .title {
    font-size: 24px;
  }
  
  .subtitle {
    font-size: 15px;
  }
}
</style>
