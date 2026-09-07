<script setup lang="ts">
import AppIcon from '../../components/AppIcon.vue'
import type { Review } from '../../data/client'

defineProps<{ validations: Review['validations']; disabled?: boolean }>()
defineEmits<{ locate: [fieldId: string] }>()
</script>

<template>
  <ul class="validation-list">
    <li v-for="validation in validations" :key="validation.id" :data-status="validation.status">
      <span><AppIcon :name="validation.status === 'passed' ? 'check' : 'alert'" /></span>
      <div>
        <strong>{{
          validation.status === 'passed'
            ? '校验通过'
            : validation.status === 'warning'
              ? '请留意'
              : '需要处理'
        }}</strong>
        <p>{{ validation.safe_message }}</p>
        <small class="technical-meta">{{ validation.rule_code }}</small>
        <button
          v-if="validation.status !== 'passed' && validation.field_claim_id"
          class="text-button validation-locate"
          type="button"
          :disabled="disabled"
          @click="$emit('locate', validation.field_claim_id)"
        >
          定位对应字段
        </button>
      </div>
    </li>
  </ul>
</template>

<style scoped>
.validation-locate {
  display: block;
  margin-top: 6px;
}
</style>
