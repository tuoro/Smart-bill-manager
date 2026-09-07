<script setup lang="ts">
import type { AllocationWorkspace } from '../../data/client'
import { formatMinorUnits } from '../facts/money'
import type { AllocationSuggestion } from './suggestion'

defineProps<{
  suggestion: AllocationSuggestion
  workspace: AllocationWorkspace
  disabled: boolean
  adopted: boolean
  edited: boolean
}>()
defineEmits<{ adopt: []; decline: [] }>()
</script>

<template>
  <section class="panel suggestion-panel" aria-labelledby="allocation-suggestion-title">
    <div class="panel-heading">
      <div>
        <h2 id="allocation-suggestion-title">分配草案（未保存）</h2>
        <p>{{ suggestion.message }}</p>
      </div>
    </div>
    <div v-if="suggestion.items.length" class="suggestion-body">
      <p class="notice notice-warning">
        同名、日期接近或金额吻合不能证明同一交易，请核对后决定。建议仅依据本次读取快照，不会自动保存。
      </p>
      <p>保留当前 {{ workspace.links.length }} 条关联及其金额，只补充以下新目标。</p>
      <ul class="suggestion-list">
        <li v-for="item in suggestion.items" :key="item.target.id">
          <strong>{{ item.target.display_name }}</strong>
          <p>
            名称一致 · 相差 {{ item.target.date_distance_days }} 天 ·
            {{ item.target.business_date }}
          </p>
          <dl>
            <div>
              <dt>目标剩余</dt>
              <dd>{{ formatMinorUnits(item.target.remaining_minor, item.target.currency) }}</dd>
            </div>
            <div>
              <dt>本次建议</dt>
              <dd>
                {{
                  item.amountMinor === null
                    ? '金额待填写'
                    : formatMinorUnits(item.amountMinor, item.target.currency)
                }}
              </dd>
            </div>
            <div>
              <dt>本次上限</dt>
              <dd>{{ formatMinorUnits(item.maximumMinor, item.target.currency) }}</dd>
            </div>
            <div>
              <dt>目标预计剩余</dt>
              <dd>
                {{
                  item.amountMinor === null
                    ? '填写后计算'
                    : formatMinorUnits(
                        item.target.remaining_minor - item.amountMinor,
                        item.target.currency,
                      )
                }}
              </dd>
            </div>
          </dl>
        </li>
      </ul>
      <p>
        当前单据剩余
        {{ formatMinorUnits(workspace.anchor.remaining_minor, workspace.anchor.currency) }} ·
        采用后预计剩余
        {{
          suggestion.remainingMinor === null
            ? '填写金额后计算'
            : formatMinorUnits(suggestion.remainingMinor, workspace.anchor.currency)
        }}
      </p>
      <p v-if="adopted" class="notice notice-info" role="status">
        已放入下方编辑区，尚未保存。可修改目标和金额，以最终编辑内容为准；仍须填写理由并明确确认。
      </p>
      <template v-else>
        <p v-if="edited" class="quiet">
          已有未保存的目标或金额输入，建议不会覆盖。可继续手工编辑，或明确刷新后重新核对。
        </p>
        <div class="page-actions">
          <button
            class="button"
            type="button"
            :disabled="disabled || edited"
            @click="$emit('adopt')"
          >
            {{
              suggestion.status === 'amount_required' ? '采用目标，金额由我填写' : '采用到编辑区'
            }}
          </button>
          <button class="text-button" type="button" :disabled="disabled" @click="$emit('decline')">
            不采用草案
          </button>
        </div>
      </template>
    </div>
  </section>
</template>

<style scoped>
.suggestion-body {
  padding: 0 20px 20px;
  display: grid;
  gap: 16px;
}
.suggestion-body > p {
  margin: 0;
}
.suggestion-list {
  margin: 0;
  padding: 0;
  list-style: none;
  display: grid;
  gap: 12px;
}
.suggestion-list li {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 16px;
}
.suggestion-list p {
  margin: 6px 0 12px;
}
.suggestion-list dl {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
  margin: 0;
}
.suggestion-list dt {
  font-size: 12px;
}
.suggestion-list dd {
  margin: 4px 0 0;
  overflow-wrap: anywhere;
}
@media (max-width: 767px) {
  .suggestion-list dl {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
