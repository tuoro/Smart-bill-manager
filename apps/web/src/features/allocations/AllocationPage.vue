<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { ApiError, api, type AllocationFactType, type AllocationWorkspace } from '../../data/client'
import { formatMinorUnits } from '../facts/money'
import {
  allocationModeLabel,
  createAllocationDraft,
  validateAllocationDraft,
  type AllocationDraftRow,
} from './model'

const route = useRoute()
const factType = computed(() => route.params.factType as AllocationFactType)
const factId = computed(() => String(route.params.factId ?? ''))
const workspace = ref<AllocationWorkspace | null>(null)
const rows = ref<AllocationDraftRow[]>([])
const reason = ref('')
const withdrawAllConfirmed = ref(false)
const loading = ref(true)
const submitting = ref(false)
const forbidden = ref(false)
const error = ref('')
const conflict = ref('')
const success = ref('')
const attempted = ref(false)

const validation = computed(() =>
  workspace.value
    ? validateAllocationDraft(workspace.value, rows.value, reason.value, withdrawAllConfirmed.value)
    : undefined,
)
const selectedCount = computed(() => rows.value.filter((row) => row.selected).length)
const modeLabel = computed(() =>
  workspace.value ? allocationModeLabel(workspace.value, rows.value) : '没有变化',
)
const canSubmit = computed(
  () => Boolean(workspace.value && validation.value?.changed) && !submitting.value,
)
const returnPath = computed(() => (factType.value === 'invoice' ? '/invoices' : '/payments'))
const returnLabel = computed(() => (factType.value === 'invoice' ? '返回发票列表' : '返回支付列表'))

async function load() {
  if (!['payment', 'invoice'].includes(factType.value) || !factId.value) {
    loading.value = false
    error.value = '分配页面地址不合法'
    return
  }
  loading.value = true
  try {
    const latest = await api.allocationWorkspace(factType.value, factId.value)
    workspace.value = latest
    rows.value = createAllocationDraft(latest)
    reason.value = ''
    withdrawAllConfirmed.value = false
    forbidden.value = false
    error.value = ''
    conflict.value = ''
    attempted.value = false
  } catch (caught) {
    forbidden.value = caught instanceof ApiError && caught.status === 403
    error.value = caught instanceof ApiError ? caught.message : '分配工作区加载失败'
  } finally {
    loading.value = false
  }
}

function toggleRow(row: AllocationDraftRow) {
  if (row.selected && !row.amountText) {
    row.amountText = row.target.current_link_id ? String(row.target.current_allocated_minor) : ''
  }
  if (selectedCount.value > 0) withdrawAllConfirmed.value = false
  attempted.value = false
  success.value = ''
}

async function submit() {
  if (!workspace.value || !validation.value) return
  attempted.value = true
  conflict.value = ''
  error.value = ''
  success.value = ''
  if (!validation.value.request) return
  submitting.value = true
  try {
    const result = await api.adjustAllocation(
      factType.value,
      factId.value,
      validation.value.request,
      `allocation-${crypto.randomUUID()}`,
    )
    success.value = `${result.mode === 'supplement' ? '补充' : result.mode === 'withdraw' ? '撤销' : '替换'}分配已保存`
    await load()
    success.value = `${result.mode === 'supplement' ? '补充' : result.mode === 'withdraw' ? '撤销' : '替换'}分配已保存，余额已刷新`
  } catch (caught) {
    if (caught instanceof ApiError && caught.status === 409) {
      conflict.value = `${caught.message}。当前草稿已保留，请刷新后重新确认。`
    } else {
      error.value = caught instanceof ApiError ? caught.message : '分配调整提交失败'
    }
  } finally {
    submitting.value = false
  }
}

onMounted(() => void load())
</script>

<template>
  <div class="page-stack allocation-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <RouterLink :to="returnPath">财务数据</RouterLink><span aria-hidden="true">/</span
      ><strong>调整分配</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>调整支付—发票分配</h1>
        <p>提交完整期望计划；金额变化会终止旧 Link 并创建新 Link。</p>
      </div>
      <RouterLink class="button button-small" :to="returnPath">{{ returnLabel }}</RouterLink>
    </header>

    <section v-if="loading" class="panel state-layout" role="status">
      <span class="spinner spinner-large" aria-hidden="true"></span
      ><strong>正在加载当前分配</strong>
    </section>
    <section v-else-if="forbidden" class="panel state-layout">
      <span class="state-glyph" aria-hidden="true">锁</span><strong>没有调整分配的权限</strong
      ><span>只有 Owner 或 Finance 可以更改已确认 Fact 的分配关系。</span>
    </section>
    <section v-else-if="!workspace" class="panel state-layout">
      <span class="state-glyph" aria-hidden="true">!</span><strong>分配工作区不可用</strong
      ><span>{{ error }}</span
      ><button class="button" type="button" @click="load">重试</button>
    </section>

    <template v-else>
      <div v-if="success" class="notice notice-success" role="status" aria-live="polite">
        <span aria-hidden="true">✓</span><strong>{{ success }}</strong>
      </div>
      <div v-if="conflict" class="notice notice-warning" role="alert">
        <span aria-hidden="true">!</span><span>{{ conflict }}</span
        ><button class="text-button" type="button" @click="load">刷新权威计划</button>
      </div>
      <div v-if="error" class="notice notice-danger" role="alert">
        <span aria-hidden="true">!</span><span>{{ error }}</span>
      </div>

      <section class="panel allocation-anchor" aria-labelledby="allocation-anchor-title">
        <div class="panel-heading">
          <div>
            <h2 id="allocation-anchor-title">
              当前 {{ workspace.anchor.fact_type === 'payment' ? '支付' : '发票' }}
            </h2>
            <p>{{ workspace.anchor.display_name }} · {{ workspace.anchor.business_date }}</p>
          </div>
          <button class="button button-small" type="button" @click="load">刷新</button>
        </div>
        <dl class="allocation-summary">
          <div>
            <dt>总额</dt>
            <dd>
              {{ formatMinorUnits(workspace.anchor.amount_minor, workspace.anchor.currency) }}
            </dd>
          </div>
          <div>
            <dt>当前已分配</dt>
            <dd>
              {{ formatMinorUnits(workspace.anchor.allocated_minor, workspace.anchor.currency) }}
            </dd>
          </div>
          <div>
            <dt>当前剩余</dt>
            <dd>
              {{ formatMinorUnits(workspace.anchor.remaining_minor, workspace.anchor.currency) }}
            </dd>
          </div>
          <div>
            <dt>活动 Link</dt>
            <dd>{{ workspace.links.length }} 条</dd>
          </div>
        </dl>
      </section>

      <form
        class="panel allocation-form"
        aria-labelledby="allocation-targets-title"
        @submit.prevent="submit"
      >
        <div class="panel-heading">
          <div>
            <h2 id="allocation-targets-title">完整期望分配计划</h2>
            <p>已选择 {{ selectedCount }} / {{ rows.length }} 个目标 · {{ modeLabel }}</p>
          </div>
        </div>

        <div v-if="rows.length === 0" class="state-layout compact">
          <span class="state-glyph" aria-hidden="true">链</span><strong>没有合格目标</strong
          ><span>当前没有同币种且日期相差不超过 30 天的相反类型 Fact。</span>
        </div>
        <ul v-else class="allocation-target-list">
          <li v-for="row in rows" :key="row.target.id" class="allocation-target-row">
            <label class="allocation-target-choice">
              <input v-model="row.selected" type="checkbox" @change="toggleRow(row)" />
              <span>
                <strong>{{ row.target.display_name }}</strong>
                <small>
                  {{ row.target.business_date }} · 相差 {{ row.target.date_distance_days }} 天 ·
                  {{ row.target.name_exact ? '名称一致' : '名称不一致，仅作提示' }}
                </small>
              </span>
            </label>
            <dl class="allocation-target-balance">
              <div>
                <dt>目标总额</dt>
                <dd>{{ formatMinorUnits(row.target.amount_minor, row.target.currency) }}</dd>
              </div>
              <div>
                <dt>其他已占用</dt>
                <dd>
                  {{
                    formatMinorUnits(
                      row.target.allocated_minor - row.target.current_allocated_minor,
                      row.target.currency,
                    )
                  }}
                </dd>
              </div>
              <div>
                <dt>当前对</dt>
                <dd>
                  {{ formatMinorUnits(row.target.current_allocated_minor, row.target.currency) }}
                </dd>
              </div>
              <div>
                <dt>可调整上限</dt>
                <dd>
                  {{ formatMinorUnits(row.target.maximum_allocatable_minor, row.target.currency) }}
                </dd>
              </div>
            </dl>
            <label class="field-stack allocation-amount">
              <span>期望分配（最小单位）</span>
              <input
                v-model="row.amountText"
                class="input numeric"
                type="text"
                inputmode="numeric"
                pattern="[0-9]*"
                :disabled="!row.selected"
                :aria-invalid="attempted && Boolean(validation?.targetErrors[row.target.id])"
                :aria-describedby="
                  validation?.targetErrors[row.target.id]
                    ? `allocation-error-${row.target.id}`
                    : undefined
                "
                @input="attempted = false"
              />
              <small v-if="row.target.current_link_id && !row.selected" class="danger-text"
                >提交后将终止当前 Link</small
              >
              <small
                v-if="attempted && validation?.targetErrors[row.target.id]"
                :id="`allocation-error-${row.target.id}`"
                class="danger-text"
                >{{ validation.targetErrors[row.target.id] }}</small
              >
            </label>
          </li>
        </ul>

        <div class="allocation-decision">
          <div class="allocation-projection" aria-live="polite">
            <span>提交后合计</span>
            <strong>{{
              formatMinorUnits(validation?.desiredTotalMinor ?? 0, workspace.anchor.currency)
            }}</strong>
            <small
              >提交后剩余
              {{
                formatMinorUnits(
                  workspace.anchor.amount_minor - (validation?.desiredTotalMinor ?? 0),
                  workspace.anchor.currency,
                )
              }}</small
            >
          </div>
          <label class="field-stack">
            <span>调整理由</span>
            <textarea
              v-model="reason"
              class="textarea"
              rows="3"
              maxlength="500"
              :aria-invalid="attempted && Boolean(validation?.reasonError)"
              :aria-describedby="
                validation?.reasonError ? 'allocation-reason-error' : 'allocation-reason-note'
              "
              @input="attempted = false"
            ></textarea>
            <small id="allocation-reason-note" class="form-note"
              >必填，最多 500 个字符；不会写入安全审计 metadata。</small
            >
            <small
              v-if="attempted && validation?.reasonError"
              id="allocation-reason-error"
              class="danger-text"
              >{{ validation.reasonError }}</small
            >
          </label>
          <label
            v-if="workspace.links.length > 0 && selectedCount === 0"
            class="allocation-withdraw-all"
          >
            <input
              v-model="withdrawAllConfirmed"
              type="checkbox"
              :aria-invalid="attempted && Boolean(validation?.withdrawAllError)"
              :aria-describedby="
                attempted && validation?.withdrawAllError ? 'allocation-withdraw-error' : undefined
              "
            />
            <span>确认撤销全部 {{ workspace.links.length }} 条活动分配</span>
          </label>
          <p
            v-if="attempted && validation?.withdrawAllError"
            id="allocation-withdraw-error"
            class="danger-text"
          >
            {{ validation.withdrawAllError }}
          </p>
          <p
            v-if="validation?.planError"
            class="form-note"
            :class="{ 'danger-text': attempted && validation.changed }"
          >
            {{ validation.planError }}
          </p>
          <div class="allocation-actions">
            <RouterLink class="button" :to="returnPath">取消</RouterLink>
            <button class="button button-primary" type="submit" :disabled="!canSubmit">
              {{ submitting ? '正在保存…' : `确认${modeLabel}` }}
            </button>
          </div>
        </div>
      </form>
    </template>
  </div>
</template>
