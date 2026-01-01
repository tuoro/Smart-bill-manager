<template>
  <div class="sbm-auth-bg">
    <div class="sbm-auth-card sbm-surface">
      <div class="header">
        <h2 class="sbm-auth-title">邀请码注册</h2>
        <p class="subtitle">需要管理员生成的邀请码才能注册</p>
      </div>

      <form class="p-fluid" @submit.prevent="handleRegister">
        <div class="field">
          <label class="sbm-field-label" for="inviteCode">邀请码</label>
          <InputText id="inviteCode" v-model.trim="form.inviteCode" autocomplete="off" />
          <small v-if="errors.inviteCode" class="p-error">{{ errors.inviteCode }}</small>
        </div>

        <div class="field">
          <label class="sbm-field-label" for="username">用户名</label>
          <InputText id="username" v-model.trim="form.username" autocomplete="username" />
          <small v-if="errors.username" class="p-error">{{ errors.username }}</small>
        </div>

        <div class="field">
          <label class="sbm-field-label" for="password">密码</label>
          <Password id="password" v-model="form.password" toggleMask :feedback="false" autocomplete="new-password" />
          <small v-if="errors.password" class="p-error">{{ errors.password }}</small>
        </div>

        <div class="field">
          <label class="sbm-field-label" for="confirmPassword">确认密码</label>
          <Password id="confirmPassword" v-model="form.confirmPassword" toggleMask :feedback="false" autocomplete="new-password" />
          <small v-if="errors.confirmPassword" class="p-error">{{ errors.confirmPassword }}</small>
        </div>

        <div class="field">
          <label class="sbm-field-label" for="email">邮箱 (可选)</label>
          <InputText id="email" v-model.trim="form.email" autocomplete="email" />
          <small v-if="errors.email" class="p-error">{{ errors.email }}</small>
        </div>

        <Button type="submit" class="submit-btn" :label="'注册并登录'" :loading="loading" />
      </form>

      <div class="footer">
        <span class="muted">已有账号？</span>
        <Button class="p-button-text" label="去登录" @click="router.push('/login')" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import InputText from 'primevue/inputtext'
import Password from 'primevue/password'
import Button from 'primevue/button'
import { useToast } from 'primevue/usetoast'
import { authApi, setStoredUser, setToken } from '@/api'

const router = useRouter()
const toast = useToast()

const loading = ref(false)
const form = reactive({
  inviteCode: '',
  username: '',
  password: '',
  confirmPassword: '',
  email: '',
})

const errors = reactive({
  inviteCode: '',
  username: '',
  password: '',
  confirmPassword: '',
  email: '',
})

const validate = () => {
  errors.inviteCode = ''
  errors.username = ''
  errors.password = ''
  errors.confirmPassword = ''
  errors.email = ''

  if (!form.inviteCode.trim()) errors.inviteCode = '请输入邀请码'

  if (!form.username) {
    errors.username = '请输入用户名'
  } else if (form.username.length < 3 || form.username.length > 50) {
    errors.username = '用户名长度应为 3-50 个字符'
  }

  if (!form.password) {
    errors.password = '请输入密码'
  } else if (form.password.length < 6) {
    errors.password = '密码长度至少 6 个字符'
  }

  if (!form.confirmPassword) {
    errors.confirmPassword = '请再次输入密码'
  } else if (form.confirmPassword !== form.password) {
    errors.confirmPassword = '两次输入的密码不一致'
  }

  if (form.email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(form.email)) {
    errors.email = '请输入有效的邮箱地址'
  }

  return !errors.inviteCode && !errors.username && !errors.password && !errors.confirmPassword && !errors.email
}

const handleRegister = async () => {
  if (!validate()) return

  loading.value = true
  try {
    const res = await authApi.inviteRegister(form.inviteCode, form.username, form.password, form.email || undefined)
    if (res.data.success) {
      if (res.data.token) setToken(res.data.token)
      if (res.data.user) setStoredUser(res.data.user)
      toast.add({ severity: 'success', summary: '注册成功', life: 2200 })
      setTimeout(() => router.push('/dashboard'), 300)
      return
    }
    toast.add({ severity: 'error', summary: res.data.message || '注册失败', life: 3500 })
  } catch (error: any) {
    toast.add({
      severity: 'error',
      summary: error.response?.data?.message || '注册失败，请稍后重试',
      life: 3500,
    })
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.header {
  text-align: center;
  margin-bottom: 18px;
}

.p-fluid {
  width: 100%;
}

.p-fluid form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.subtitle {
  margin: 10px 0 0;
  color: var(--p-text-muted-color);
  font-size: 13px;
  font-weight: 700;
}

.field {
  margin-bottom: 0;
  display: flex;
  flex-direction: column;
}

.field label {
  display: block;
  margin-bottom: 6px;
  font-weight: 600;
  color: var(--color-text-secondary);
}

.field :deep(.p-inputtext),
.field :deep(.p-password),
.field :deep(.p-password input) {
  width: 100%;
}

.submit-btn {
  width: 100%;
  height: 46px;
  border-radius: var(--radius-md);
  font-weight: 700;
}

.footer {
  margin-top: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
}

.muted {
  color: var(--p-text-muted-color);
}
</style>
