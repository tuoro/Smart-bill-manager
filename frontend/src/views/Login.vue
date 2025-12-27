<template>
  <div class="sbm-auth-bg">
    <div class="sbm-auth-card sbm-surface">
      <div class="header">
        <h2 class="sbm-auth-title">&#128176; &#26234;&#33021;&#36134;&#21333;&#31649;&#29702;</h2>
        <p class="sbm-auth-subtitle">Smart Bill Manager</p>
      </div>

      <form class="p-fluid" @submit.prevent="handleLogin">
        <div class="field">
          <label class="sbm-field-label" for="username">&#29992;&#25143;&#21517;</label>
          <InputText id="username" v-model.trim="form.username" autocomplete="username" />
        </div>

        <div class="field">
          <label class="sbm-field-label" for="password">&#23494;&#30721;</label>
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
.header {
  text-align: center;
  margin-bottom: 22px;
}

form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.field {
  margin-bottom: 0;
  display: flex;
  flex-direction: column;
}

.field :deep(.p-inputtext) {
  width: 100%;
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

@media (max-width: 480px) {
  .sbm-auth-card {
    padding: 22px 18px;
  }
}
</style>
