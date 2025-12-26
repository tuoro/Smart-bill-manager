<template>
  <Dialog
    v-model:visible="visible"
    modal
    :header="'\u4FEE\u6539\u5BC6\u7801'"
    :style="{ width: '520px', maxWidth: '92vw' }"
    :closable="!loading"
    :closeOnEscape="!loading"
    @hide="handleClose"
  >
    <form class="p-fluid" @submit.prevent="handleSubmit">
      <div class="field">
        <label for="oldPassword">&#21407;&#23494;&#30721;</label>
        <Password
          id="oldPassword"
          v-model="form.oldPassword"
          toggleMask
          :feedback="false"
          autocomplete="current-password"
        />
        <small v-if="errors.oldPassword" class="p-error">{{ errors.oldPassword }}</small>
      </div>

      <div class="field">
        <label for="newPassword">&#26032;&#23494;&#30721;</label>
        <Password
          id="newPassword"
          v-model="form.newPassword"
          toggleMask
          :feedback="false"
          autocomplete="new-password"
          @input="updatePasswordStrength"
        />
        <small v-if="errors.newPassword" class="p-error">{{ errors.newPassword }}</small>
        <div v-if="form.newPassword" class="password-strength">
          <span :class="['strength-indicator', passwordStrength.level]">
            &#23494;&#30721;&#24378;&#24230;: {{ passwordStrength.text }}
          </span>
        </div>
      </div>

      <div class="field">
        <label for="confirmPassword">&#30830;&#35748;&#26032;&#23494;&#30721;</label>
        <Password
          id="confirmPassword"
          v-model="form.confirmPassword"
          toggleMask
          :feedback="false"
          autocomplete="new-password"
        />
        <small v-if="errors.confirmPassword" class="p-error">{{ errors.confirmPassword }}</small>
      </div>

      <div class="footer">
        <Button
          type="button"
          :label="'\u53D6\u6D88'"
          severity="secondary"
          class="p-button-outlined"
          :disabled="loading"
          @click="handleClose"
        />
        <Button type="submit" :label="'\u786E\u5B9A\u4FEE\u6539'" :loading="loading" />
      </div>
    </form>
  </Dialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import Dialog from 'primevue/dialog'
import Button from 'primevue/button'
import Password from 'primevue/password'
import { useToast } from 'primevue/usetoast'
import { authApi } from '@/api/auth'
import { checkPasswordStrength, type PasswordStrength } from '@/utils/password'

interface Props {
  modelValue: boolean
}

interface Emits {
  (e: 'update:modelValue', value: boolean): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const toast = useToast()
const loading = ref(false)
const visible = ref(props.modelValue)

const form = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: '',
})

const errors = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: '',
})

const passwordStrength = ref<PasswordStrength>({
  level: 'weak',
  text: '\u5F31',
})

const updatePasswordStrength = () => {
  passwordStrength.value = checkPasswordStrength(form.newPassword)
}

const validate = () => {
  errors.oldPassword = ''
  errors.newPassword = ''
  errors.confirmPassword = ''

  if (!form.oldPassword) errors.oldPassword = '\u8BF7\u8F93\u5165\u539F\u5BC6\u7801'

  if (!form.newPassword) {
    errors.newPassword = '\u8BF7\u8F93\u5165\u65B0\u5BC6\u7801'
  } else if (form.newPassword.length < 6) {
    errors.newPassword = '\u65B0\u5BC6\u7801\u957F\u5EA6\u81F3\u5C11 6 \u4E2A\u5B57\u7B26'
  } else if (form.newPassword === form.oldPassword) {
    errors.newPassword = '\u65B0\u5BC6\u7801\u4E0D\u80FD\u4E0E\u539F\u5BC6\u7801\u76F8\u540C'
  }

  if (!form.confirmPassword) {
    errors.confirmPassword = '\u8BF7\u518D\u6B21\u8F93\u5165\u65B0\u5BC6\u7801'
  } else if (form.confirmPassword !== form.newPassword) {
    errors.confirmPassword = '\u4E24\u6B21\u8F93\u5165\u7684\u5BC6\u7801\u4E0D\u4E00\u81F4'
  }

  return !errors.oldPassword && !errors.newPassword && !errors.confirmPassword
}

watch(
  () => props.modelValue,
  (val) => {
    visible.value = val
  },
)

watch(visible, (val) => {
  emit('update:modelValue', val)
})

const handleClose = () => {
  form.oldPassword = ''
  form.newPassword = ''
  form.confirmPassword = ''
  errors.oldPassword = ''
  errors.newPassword = ''
  errors.confirmPassword = ''
  visible.value = false
}

const handleSubmit = async () => {
  if (!validate()) return

  loading.value = true
  try {
    const response = await authApi.changePassword(form.oldPassword, form.newPassword)
    if (response.data.success) {
      toast.add({ severity: 'success', summary: '\u5BC6\u7801\u4FEE\u6539\u6210\u529F', life: 2000 })
      handleClose()
      return
    }
    toast.add({ severity: 'error', summary: response.data.message || '\u4FEE\u6539\u5931\u8D25', life: 3000 })
  } catch (error: any) {
    console.error('Change password error:', error)
    toast.add({
      severity: 'error',
      summary: error.response?.data?.message || '\u4FEE\u6539\u5BC6\u7801\u5931\u8D25\uFF0C\u8BF7\u7A0D\u540E\u91CD\u8BD5',
      life: 3500,
    })
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
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

.footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 18px;
}
</style>

