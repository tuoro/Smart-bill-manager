<script setup lang="ts">
import { onMounted, ref } from 'vue'
import AppIcon from '../../components/AppIcon.vue'
import { ApiError, api, type ProviderConfig } from '../../data/client'

const items = ref<ProviderConfig[]>([])
const loading = ref(true)
const loadError = ref('')
const error = ref('')
const actionId = ref('')
const creating = ref(false)
// 正在编辑的配置。编辑与新建共用同一张表单：回填三项连接参数，密钥留空表示沿用。
const editing = ref<ProviderConfig>()
const baseUrl = ref('')
const model = ref('')
const apiKey = ref('')
const outputMode = ref<ProviderConfig['output_mode']>('json_schema')
const capabilityLabels: Record<ProviderConfig['capability_status'], string> = {
  pending: '待检测',
  passed: '检测通过',
  failed: '检测失败',
}

function activationNote(item: ProviderConfig) {
  if (actionId.value === item.id) return '操作进行中，请稍候。'
  if (item.capability_status === 'failed') return '检测未通过，请检查配置后重新检测。'
  if (item.capability_status === 'pending') return '完成能力检测后，即可启用此配置。'
  if (item.active) return '后续上传的单据将使用此配置识别。'
  return '检测已通过，可以启用此配置。'
}

async function load() {
  loading.value = true
  try {
    items.value = (await api.providerConfigs()).items
    loadError.value = ''
  } catch (caught) {
    loadError.value = caught instanceof ApiError ? caught.message : 'AI 配置加载失败'
  } finally {
    loading.value = false
  }
}

function resetForm() {
  editing.value = undefined
  apiKey.value = ''
  baseUrl.value = ''
  model.value = ''
  outputMode.value = 'json_schema'
}

function startEdit(item: ProviderConfig) {
  editing.value = item
  baseUrl.value = item.base_url
  model.value = item.model
  outputMode.value = item.output_mode
  apiKey.value = ''
  error.value = ''
  document.getElementById('provider-form-title')?.scrollIntoView({ block: 'nearest' })
}

async function submit() {
  creating.value = true
  error.value = ''
  try {
    if (editing.value) {
      const updated = await api.updateProvider(
        editing.value.id,
        baseUrl.value,
        apiKey.value,
        model.value,
        outputMode.value,
      )
      // 编辑会停用该配置，其他配置的启用状态不受影响。
      replace(updated)
      resetForm()
    } else {
      const created = await api.createProvider(
        baseUrl.value,
        apiKey.value,
        model.value,
        outputMode.value,
      )
      items.value.unshift(created)
      resetForm()
    }
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : 'AI 配置保存失败'
  } finally {
    apiKey.value = ''
    creating.value = false
  }
}

async function remove(item: ProviderConfig) {
  const warning = item.active
    ? `「${item.model}」正在使用中，删除后需要启用另一组配置才能继续识别。确定删除？`
    : `确定删除「${item.model}」？密钥会立即清除，已有识别记录保留。`
  if (!window.confirm(warning)) return
  actionId.value = item.id
  error.value = ''
  try {
    await api.deleteProvider(item.id)
    items.value = items.value.filter((entry) => entry.id !== item.id)
    if (editing.value?.id === item.id) resetForm()
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '删除配置失败'
  } finally {
    actionId.value = ''
  }
}

// HMAC 指纹 64 位十六进制，人只用它比对「换没换」，前 12 位足够，完整值放在 title。
function shortFingerprint(value: string) {
  return value.length > 12 ? value.slice(0, 12) : value
}

async function detect(item: ProviderConfig) {
  actionId.value = item.id
  error.value = ''
  try {
    replace(await api.detectProvider(item.id))
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '能力检测失败'
  } finally {
    actionId.value = ''
  }
}

async function activate(item: ProviderConfig) {
  actionId.value = item.id
  error.value = ''
  try {
    const updated = await api.activateProvider(item.id)
    items.value = items.value.map((entry) => ({ ...entry, active: entry.id === updated.id }))
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : '启用配置失败'
  } finally {
    actionId.value = ''
  }
}

function replace(updated: ProviderConfig) {
  const index = items.value.findIndex((entry) => entry.id === updated.id)
  if (index >= 0) items.value[index] = updated
}

onMounted(() => void load())
</script>

<template>
  <div class="page-stack settings-page">
    <nav class="breadcrumb" aria-label="面包屑">
      <span>系统</span><span aria-hidden="true">/</span><strong>AI 配置</strong>
    </nav>
    <header class="page-header">
      <div>
        <h1>AI 配置</h1>
        <p>连接你的多模态模型，用于提取单据中的数字与文字。</p>
      </div>
    </header>
    <div class="provider-overview">
      <ol class="provider-step-guide" aria-label="配置使用步骤">
        <li>
          <span aria-hidden="true">1</span>
          <div><strong>保存配置</strong><small>填写接口、模型与密钥</small></div>
        </li>
        <li>
          <span aria-hidden="true">2</span>
          <div><strong>能力检测</strong><small>检查图片输入与输出格式</small></div>
        </li>
        <li>
          <span aria-hidden="true">3</span>
          <div><strong>启用配置</strong><small>用于后续单据识别</small></div>
        </li>
      </ol>
    </div>
    <div v-if="error" class="notice notice-danger" role="alert">
      <span aria-hidden="true">!</span><span>{{ error }}</span>
    </div>
    <div class="settings-grid">
      <section class="panel" aria-labelledby="provider-list-title">
        <div class="panel-heading">
          <div>
            <h2 id="provider-list-title">已有配置</h2>
            <p>可保存多组配置，同一时间仅使用其中一组。</p>
          </div>
          <span v-if="!loading && !loadError" class="provider-summary"
            >{{ items.length }} 个配置</span
          >
        </div>
        <div v-if="loadError" class="notice notice-danger provider-load-error" role="alert">
          <span aria-hidden="true">!</span>
          <span>{{ loadError }}，当前列表可能不完整。</span>
          <button class="text-button" type="button" :disabled="loading" @click="load">
            重新加载
          </button>
        </div>
        <div v-if="loading" class="state-layout" role="status">
          <span class="spinner spinner-large" aria-hidden="true"></span
          ><strong>正在读取配置</strong>
        </div>
        <div v-else-if="!loadError && items.length === 0" class="state-layout compact">
          <AppIcon class="state-glyph" name="settings" />
          <strong>连接你的第一个模型</strong>
          <span>在「添加配置」中填写服务信息，保存后即可检测。</span>
        </div>
        <ul v-else-if="items.length > 0" class="provider-list">
          <li v-for="item in items" :key="item.id">
            <div class="provider-main">
              <div>
                <strong>{{ item.model }}</strong
                ><span
                  class="status"
                  :data-tone="
                    item.capability_status === 'failed'
                      ? 'danger'
                      : item.capability_status === 'passed'
                        ? 'success'
                        : 'neutral'
                  "
                  ><span aria-hidden="true">●</span
                  >{{ capabilityLabels[item.capability_status] }}</span
                >
                <span v-if="item.active" class="status" data-tone="info"
                  ><AppIcon name="check" />使用中</span
                >
              </div>
              <p>{{ item.base_url }}</p>
              <div class="provider-meta">
                <span>版本 {{ item.version }}</span>
                <span>{{
                  item.output_mode === 'json_schema' ? '严格结构化输出' : 'JSON 对象输出'
                }}</span>
                <span :title="item.safe_fingerprint"
                  >指纹 {{ shortFingerprint(item.safe_fingerprint) }}</span
                >
              </div>
              <small v-if="item.capability_safe_message">{{ item.capability_safe_message }}</small>
              <p :id="`provider-action-note-${item.id}`" class="provider-action-note">
                {{ activationNote(item) }}
              </p>
            </div>
            <div class="provider-actions">
              <button
                class="button button-small"
                type="button"
                :disabled="actionId === item.id"
                @click="detect(item)"
              >
                能力检测</button
              ><button
                class="button button-small button-primary"
                type="button"
                :disabled="
                  item.capability_status !== 'passed' || item.active || actionId === item.id
                "
                :aria-describedby="`provider-action-note-${item.id}`"
                @click="activate(item)"
              >
                激活</button
              ><button
                class="button button-small"
                type="button"
                :disabled="actionId === item.id"
                @click="startEdit(item)"
              >
                编辑</button
              ><button
                class="button button-small button-danger"
                type="button"
                :disabled="actionId === item.id"
                @click="remove(item)"
              >
                删除
              </button>
            </div>
          </li>
        </ul>
      </section>
      <section class="panel provider-form-panel" aria-labelledby="provider-form-title">
        <div class="panel-heading">
          <div>
            <h2 id="provider-form-title">{{ editing ? '编辑配置' : '添加配置' }}</h2>
            <p v-if="editing">正在修改「{{ editing.model }}」。保存后需要重新检测并启用。</p>
            <p v-else>支持兼容 OpenAI 接口的多模态模型。</p>
          </div>
          <button v-if="editing" class="text-button" type="button" @click="resetForm">
            取消编辑
          </button>
        </div>
        <form class="stack-form" @submit.prevent="submit">
          <label class="field-stack"
            ><span>接口地址 <small>Base URL</small></span
            ><input
              v-model.trim="baseUrl"
              class="input"
              type="url"
              placeholder="https://provider.example/v1"
              maxlength="2048"
              required /></label
          ><label class="field-stack"
            ><span>模型名称 <small>Model</small></span
            ><input
              v-model.trim="model"
              class="input"
              type="text"
              placeholder="填写供应商提供的模型名称"
              maxlength="200"
              autocomplete="off"
              required /></label
          ><label class="field-stack"
            ><span>输出模式 <small>Output Mode</small></span
            ><select v-model="outputMode" class="select" aria-describedby="provider-mode-note">
              <option value="json_schema">严格结构化输出（json_schema）</option>
              <option value="json_object">JSON 对象输出（json_object）</option>
            </select></label
          >
          <p id="provider-mode-note" class="form-note">
            按供应商支持的能力选择；两种模式都会在本地校验，系统不会自动切换。
          </p>
          <label class="field-stack"
            ><span>接口密钥 <small>API Key</small></span
            ><input
              v-model="apiKey"
              class="input"
              type="password"
              maxlength="4096"
              autocomplete="off"
              :required="!editing"
              :placeholder="editing ? '留空则沿用已保存的密钥' : ''"
              aria-describedby="provider-key-note"
          /></label>
          <p id="provider-key-note" class="form-note">
            {{
              editing
                ? '留空则沿用已保存的密钥；填写即替换。密钥加密保存，不会回显。'
                : '密钥加密保存，提交后不会在页面回显。'
            }}
          </p>
          <button class="button button-primary button-block" type="submit" :disabled="creating">
            {{ creating ? '正在保存…' : editing ? '保存修改' : '创建待检测配置' }}
          </button>
          <div class="provider-security-note">
            <AppIcon name="shield" />
            <p>
              保存不会发起识别。点击「能力检测」会请求供应商，可能产生费用；通过检测后需手动启用。
            </p>
          </div>
        </form>
      </section>
    </div>
  </div>
</template>
