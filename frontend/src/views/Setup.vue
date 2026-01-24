<template>
  <div class="sbm-auth-bg">
    <div class="sbm-auth-card sbm-surface">
      <div class="header">
        <h2 class="sbm-auth-title">&#128176; &#21021;&#22987;&#21270;&#35774;&#32622;</h2>
        <p class="subtitle">&#27426;&#36814;&#20351;&#29992;&#26234;&#33021;&#36134;&#21333;&#31649;&#29702;&#31995;&#32479;</p>
        <p class="desc">&#35831;&#21019;&#24314;&#31649;&#29702;&#21592;&#36134;&#25143;&#20197;&#24320;&#22987;&#20351;&#29992;</p>
      </div>

      <form class="p-fluid" @submit.prevent="handleSetup">
        <div class="field">
          <label class="sbm-field-label" for="username">&#29992;&#25143;&#21517;</label>
          <InputText id="username" v-model.trim="form.username" autocomplete="username" />
          <small v-if="errors.username" class="p-error">{{ errors.username }}</small>
        </div>

        <div class="field">
          <label class="sbm-field-label" for="password">&#23494;&#30721;</label>
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
          <label class="sbm-field-label" for="confirmPassword">&#30830;&#35748;&#23494;&#30721;</label>
          <Password
            id="confirmPassword"
            v-model="form.confirmPassword"
            toggleMask
            :feedback="false"
            autocomplete="new-password"
          />
          <small v-if="errors.confirmPassword" class="p-error">{{ errors.confirmPassword }}</small>
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
import { authApi } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'
import { checkPasswordStrength, type PasswordStrength } from '@/utils/password'

const router = useRouter()
const toast = useToast()
const authStore = useAuthStore()

const loading = ref(false)
const form = reactive({
  username: '',
  password: '',
  confirmPassword: '',
})

const errors = reactive({
  username: '',
  password: '',
  confirmPassword: '',
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

  return !errors.username && !errors.password && !errors.confirmPassword
}

const handleSetup = async () => {
  if (!validate()) return

  loading.value = true
  try {
    const response = await authApi.setup(form.username, form.password)
    if (response.data.success) {
      if (response.data.token && response.data.user) {
        authStore.setSession(response.data.user, response.data.token)
      } else {
        toast.add({ severity: 'error', summary: '\u521B\u5EFA\u6210\u529F\uFF0C\u4F46\u8FD4\u56DE\u6570\u636E\u4E0D\u5B8C\u6574\uFF0C\u8BF7\u5237\u65B0\u540E\u91CD\u8BD5', life: 3500 })
        return
      }
      toast.add({ severity: 'success', summary: '\u7BA1\u7406\u5458\u8D26\u6237\u521B\u5EFA\u6210\u529F', life: 2200 })
      setTimeout(() => router.replace('/dashboard'), 300)
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
.header {
  text-align: center;
  margin-bottom: 18px;
}

.subtitle {
  margin: 10px 0 0;
  color: var(--p-text-color);
  font-size: 14px;
  font-weight: 800;
}

.desc {
  margin: 6px 0 0;
  color: var(--p-text-muted-color);
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
}

  :deep(.p-password input) {
    width: 100%;
  }

  :deep(.p-password) {
    width: 100%;
  }

  :deep(.p-inputtext) {
    width: 100%;
  }

  @media (max-width: 480px) {
    .sbm-auth-card {
      padding: 22px 18px;
    }
}
</style>
