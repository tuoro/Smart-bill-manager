<template>
  <div class="auth-page">
    <div class="auth-card">
      <div class="header">
        <h2 class="title">&#128176; &#26234;&#33021;&#36134;&#21333;&#31649;&#29702;</h2>
        <p class="subtitle">Smart Bill Manager</p>
      </div>

      <form class="p-fluid" @submit.prevent="handleLogin">
        <div class="field">
          <label for="username">&#29992;&#25143;&#21517;</label>
          <span class="p-input-icon-left">
            <i class="pi pi-user" />
            <InputText id="username" v-model.trim="form.username" autocomplete="username" />
          </span>
        </div>

        <div class="field">
          <label for="password">&#23494;&#30721;</label>
          <Password
            id="password"
            v-model="form.password"
            toggleMask
            :feedback="false"
            autocomplete="current-password"
          />
        </div>

        <Button type="submit" class="submit-btn" :label="'\u767B\u5F55'" :loading="loading" />
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
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const authStore = useAuthStore()
const toast = useToast()

const loading = ref(false)
const form = reactive({
  username: '',
  password: '',
})

const handleLogin = async () => {
  if (!form.username) {
    toast.add({ severity: 'warn', summary: '\u8BF7\u8F93\u5165\u7528\u6237\u540D', life: 2500 })
    return
  }
  if (!form.password) {
    toast.add({ severity: 'warn', summary: '\u8BF7\u8F93\u5165\u5BC6\u7801', life: 2500 })
    return
  }

  loading.value = true
  try {
    const result = await authStore.login(form.username, form.password)
    if (result.success) {
      toast.add({ severity: 'success', summary: '\u767B\u5F55\u6210\u529F', life: 1800 })
      router.push('/dashboard')
      return
    }
    toast.add({ severity: 'error', summary: result.message || '\u767B\u5F55\u5931\u8D25', life: 3500 })
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
  width: 420px;
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
  margin-bottom: 22px;
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
  color: var(--color-text-tertiary);
  font-size: 13px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 1px;
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

