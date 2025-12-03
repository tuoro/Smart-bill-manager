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
            @input="checkPasswordStrength"
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
import { ref, reactive, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { User, Lock, Message } from '@element-plus/icons-vue'
import { authApi, setToken, setStoredUser } from '@/api/auth'

const router = useRouter()

const formRef = ref<FormInstance>()
const loading = ref(false)

const form = reactive({
  username: '',
  password: '',
  confirmPassword: '',
  email: ''
})

const passwordStrength = ref({
  level: 'weak',
  text: '弱'
})

const checkPasswordStrength = () => {
  const pwd = form.password
  if (pwd.length < 6) {
    passwordStrength.value = { level: 'weak', text: '弱' }
  } else if (pwd.length < 10) {
    passwordStrength.value = { level: 'medium', text: '中等' }
  } else if (pwd.length >= 10 && /[A-Z]/.test(pwd) && /[0-9]/.test(pwd) && /[^A-Za-z0-9]/.test(pwd)) {
    passwordStrength.value = { level: 'strong', text: '强' }
  } else {
    passwordStrength.value = { level: 'medium', text: '中等' }
  }
}

const validatePassword = (rule: any, value: string, callback: any) => {
  if (value === '') {
    callback(new Error('请输入密码'))
  } else if (value.length < 6) {
    callback(new Error('密码长度至少6个字符'))
  } else {
    callback()
  }
}

const validateConfirmPassword = (rule: any, value: string, callback: any) => {
  if (value === '') {
    callback(new Error('请再次输入密码'))
  } else if (value !== form.password) {
    callback(new Error('两次输入密码不一致'))
  } else {
    callback()
  }
}

const validateEmail = (rule: any, value: string, callback: any) => {
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
}

.setup-card {
  width: 480px;
  border-radius: 12px;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.2);
}

.setup-header {
  text-align: center;
  margin-bottom: 24px;
}

.title {
  margin: 0;
  color: #1890ff;
  font-size: 26px;
}

.subtitle {
  margin: 8px 0 0;
  color: #666;
  font-size: 16px;
}

.description {
  margin: 8px 0 0;
  color: #999;
  font-size: 14px;
}

.password-strength {
  margin-top: 8px;
}

.strength-indicator {
  font-size: 12px;
  padding: 2px 8px;
  border-radius: 4px;
}

.strength-indicator.weak {
  color: #f56c6c;
  background-color: #fef0f0;
}

.strength-indicator.medium {
  color: #e6a23c;
  background-color: #fdf6ec;
}

.strength-indicator.strong {
  color: #67c23a;
  background-color: #f0f9ff;
}

.setup-button {
  width: 100%;
  height: 44px;
  font-size: 16px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  border: none;
}

.setup-button:hover {
  background: linear-gradient(135deg, #5a6fd6 0%, #6a4291 100%);
}
</style>
