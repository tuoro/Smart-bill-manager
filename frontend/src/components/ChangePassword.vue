<template>
  <el-dialog
    v-model="visible"
    title="修改密码"
    width="500px"
    :close-on-click-modal="false"
    @close="handleClose"
  >
    <el-form
      ref="formRef"
      :model="form"
      :rules="rules"
      label-width="100px"
      @submit.prevent="handleSubmit"
    >
      <el-form-item label="原密码" prop="oldPassword">
        <el-input
          v-model="form.oldPassword"
          type="password"
          placeholder="请输入原密码"
          show-password
          autocomplete="current-password"
        />
      </el-form-item>
      
      <el-form-item label="新密码" prop="newPassword">
        <el-input
          v-model="form.newPassword"
          type="password"
          placeholder="请输入新密码 (至少6位)"
          show-password
          autocomplete="new-password"
          @input="updatePasswordStrength"
        />
        <div v-if="form.newPassword" class="password-strength">
          <span :class="['strength-indicator', passwordStrength.level]">
            密码强度: {{ passwordStrength.text }}
          </span>
        </div>
      </el-form-item>
      
      <el-form-item label="确认新密码" prop="confirmPassword">
        <el-input
          v-model="form.confirmPassword"
          type="password"
          placeholder="请再次输入新密码"
          show-password
          autocomplete="new-password"
        />
      </el-form-item>
    </el-form>
    
    <template #footer>
      <el-button @click="handleClose">取消</el-button>
      <el-button type="primary" :loading="loading" @click="handleSubmit">
        确定修改
      </el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
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

const formRef = ref<FormInstance>()
const loading = ref(false)
const visible = ref(props.modelValue)

const form = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})

const passwordStrength = ref<PasswordStrength>({
  level: 'weak',
  text: '弱'
})

const updatePasswordStrength = () => {
  passwordStrength.value = checkPasswordStrength(form.newPassword)
}

const validateNewPassword = (_rule: any, value: string, callback: any) => {
  if (value === '') {
    callback(new Error('请输入新密码'))
  } else if (value.length < 6) {
    callback(new Error('新密码长度至少6个字符'))
  } else if (value === form.oldPassword) {
    callback(new Error('新密码不能与原密码相同'))
  } else {
    callback()
  }
}

const validateConfirmPassword = (_rule: any, value: string, callback: any) => {
  if (value === '') {
    callback(new Error('请再次输入新密码'))
  } else if (value !== form.newPassword) {
    callback(new Error('两次输入密码不一致'))
  } else {
    callback()
  }
}

const rules: FormRules = {
  oldPassword: [
    { required: true, message: '请输入原密码', trigger: 'blur' }
  ],
  newPassword: [
    { required: true, validator: validateNewPassword, trigger: 'blur' }
  ],
  confirmPassword: [
    { required: true, validator: validateConfirmPassword, trigger: 'blur' }
  ]
}

watch(() => props.modelValue, (val) => {
  visible.value = val
})

watch(visible, (val) => {
  emit('update:modelValue', val)
})

const handleClose = () => {
  formRef.value?.resetFields()
  form.oldPassword = ''
  form.newPassword = ''
  form.confirmPassword = ''
  visible.value = false
}

const handleSubmit = async () => {
  if (!formRef.value) return
  
  await formRef.value.validate(async (valid) => {
    if (!valid) return
    
    loading.value = true
    try {
      const response = await authApi.changePassword(form.oldPassword, form.newPassword)
      
      if (response.data.success) {
        ElMessage.success('密码修改成功')
        handleClose()
      } else {
        ElMessage.error(response.data.message || '修改失败')
      }
    } catch (error: any) {
      console.error('Change password error:', error)
      ElMessage.error(error.response?.data?.message || '修改密码失败，请稍后重试')
    } finally {
      loading.value = false
    }
  })
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

/* Form enhancements */
:deep(.el-form-item__label) {
  font-weight: 500;
  color: var(--color-text-primary);
}

:deep(.el-input__wrapper) {
  border-radius: var(--radius-md);
  transition: all var(--transition-base);
}

:deep(.el-input__wrapper:hover) {
  box-shadow: var(--shadow-sm);
}

:deep(.el-input__wrapper.is-focus) {
  box-shadow: 0 0 0 3px rgba(102, 126, 234, 0.1);
}
</style>
