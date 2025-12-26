<template>
  <div class="auth-page">
    <div class="auth-card">
      <div class="header">
        <h2 class="title">&#128176; &#21021;&#22987;&#21270;&#35774;&#32622;</h2>
        <p class="subtitle">&#27426;&#36814;&#20351;&#29992;&#26234;&#33021;&#36134;&#21333;&#31649;&#29702;&#31995;&#32479;</p>
        <p class="desc">&#35831;&#21019;&#24314;&#31649;&#29702;&#21592;&#36134;&#25143;&#20197;&#24320;&#22987;&#20351;&#29992;</p>
      </div>

      <form class="p-fluid" @submit.prevent="handleSetup">
        <div class="field">
          <label for="username">&#29992;&#25143;&#21517;</label>
          <span class="p-input-icon-left">
            <i class="pi pi-user" />
            <InputText id="username" v-model.trim="form.username" autocomplete="username" />
          </span>
          <small v-if="errors.username" class="p-error">{{ errors.username }}</small>
        </div>

        <div class="field">
          <label for="password">&#23494;&#30721;</label>
          <Password
            id="password"
            v-model="form.password"
            toggleMask
            :feedback="false"
            autocomplete="new-password"
            @input="updatePasswordStrength"
          />
          <small v-if="errors.password" class="p-error">{{ errors.password }}</small>
          <div v-if="form.password" class="password-strength">
            <span :class="['strength-indicator', passwordStrength.level]">
              &#23494;&#30721;&#24378;&#24230;: {{ passwordStrength.text }}
            </span>
          </div>
        </div>

        <div class="field">
          <label for="confirmPassword">&#30830;&#35748;&#23494;&#30721;</label>
          <Password
            id="confirmPassword"
            v-model="form.confirmPassword"
            toggleMask
            :feedback="false"
            autocomplete="new-password"
          />
          <small v-if="errors.confirmPassword" class="p-error">{{ errors.confirmPassword }}</small>
        </div>

        <div class="field">
          <label for="email">&#37038;&#31665; (&#21487;&#36873;)</label>
          <span class="p-input-icon-left">
            <i class="pi pi-envelope" />
            <InputText id="email" v-model.trim="form.email" autocomplete="email" />
          </span>
          <small v-if="errors.email" class="p-error">{{ errors.email }}</small>
        </div>

        <Button type="submit" class="submit-btn" :label="'\u521B\u5EFA\u7BA1\u7406\u5458\u8D26\u6237'" :loading="loading" />
      </form>
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
import { authApi, setStoredUser, setToken } from '@/api/auth'
import { checkPasswordStrength, type PasswordStrength } from '@/utils/password'

const router = useRouter()
const toast = useToast()

const loading = ref(false)
const form = reactive({
  username: '',
  password: '',
  confirmPassword: '',
  email: '',
})

const errors = reactive({
  username: '',
  password: '',
  confirmPassword: '',
  email: '',
})

const passwordStrength = ref<PasswordStrength>({
  level: 'weak',
  text: '\u5F31',
})

const updatePasswordStrength = () => {
  passwordStrength.value = checkPasswordStrength(form.password)
}

const validate = () => {
  errors.username = ''
  errors.password = ''
  errors.confirmPassword = ''
  errors.email = ''

  if (!form.username) {
    errors.username = '\u8BF7\u8F93\u5165\u7528\u6237\u540D'
  } else if (form.username.length < 3 || form.username.length > 50) {
    errors.username = '\u7528\u6237\u540D\u957F\u5EA6\u5E94\u4E3A 3-50 \u4E2A\u5B57\u7B26'
  }

  if (!form.password) {
    errors.password = '\u8BF7\u8F93\u5165\u5BC6\u7801'
  } else if (form.password.length < 6) {
    errors.password = '\u5BC6\u7801\u957F\u5EA6\u81F3\u5C11 6 \u4E2A\u5B57\u7B26'
  }

  if (!form.confirmPassword) {
    errors.confirmPassword = '\u8BF7\u518D\u6B21\u8F93\u5165\u5BC6\u7801'
  } else if (form.confirmPassword !== form.password) {
    errors.confirmPassword = '\u4E24\u6B21\u8F93\u5165\u7684\u5BC6\u7801\u4E0D\u4E00\u81F4'
  }

  if (form.email && !/^[^\\s@]+@[^\\s@]+\\.[^\\s@]+$/.test(form.email)) {
    errors.email = '\u8BF7\u8F93\u5165\u6709\u6548\u7684\u90AE\u7BB1\u5730\u5740'
  }

  return !errors.username && !errors.password && !errors.confirmPassword && !errors.email
}

const handleSetup = async () => {
  if (!validate()) return

  loading.value = true
  try {
    const response = await authApi.setup(form.username, form.password, form.email || undefined)
    if (response.data.success) {
      if (response.data.token) setToken(response.data.token)
      if (response.data.user) setStoredUser(response.data.user)
      toast.add({ severity: 'success', summary: '\u7BA1\u7406\u5458\u8D26\u6237\u521B\u5EFA\u6210\u529F', life: 2200 })
      setTimeout(() => router.push('/dashboard'), 300)
      return
    }
    toast.add({ severity: 'error', summary: response.data.message || '\u521B\u5EFA\u5931\u8D25', life: 3500 })
  } catch (error: any) {
    console.error('Setup error:', error)
    toast.add({
      severity: 'error',
      summary: error.response?.data?.message || '\u521B\u5EFA\u5931\u8D25\uFF0C\u8BF7\u7A0D\u540E\u91CD\u8BD5',
      life: 3500,
    })
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.auth-page {
  min-height: 100vh;
  display: grid;
  place-items: center;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  position: relative;
  overflow: hidden;
  padding: 20px;
}

.auth-page::before {
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

.auth-card {
  width: 520px;
  max-width: 92vw;
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-xl);
  background: rgba(255, 255, 255, 0.92);
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
  border: 1px solid rgba(255, 255, 255, 0.25);
  position: relative;
  z-index: 1;
  padding: 30px 26px;
}

.header {
  text-align: center;
  margin-bottom: 18px;
}

.title {
  margin: 0;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  font-size: 26px;
  font-weight: 800;
  letter-spacing: -0.4px;
}

.subtitle {
  margin: 10px 0 0;
  color: var(--color-text-primary);
  font-size: 14px;
  font-weight: 700;
}

.desc {
  margin: 6px 0 0;
  color: var(--color-text-tertiary);
  font-size: 13px;
}

.field {
  margin-bottom: 14px;
}

.field label {
  display: block;
  margin-bottom: 6px;
  font-weight: 600;
  color: var(--color-text-secondary);
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

.submit-btn {
  width: 100%;
  height: 46px;
  border-radius: var(--radius-md);
  font-weight: 700;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  border: none;
}

:deep(.p-password input) {
  width: 100%;
}

@media (max-width: 480px) {
  .auth-card {
    padding: 24px 18px;
  }
  .title {
    font-size: 22px;
  }
}
</style>

