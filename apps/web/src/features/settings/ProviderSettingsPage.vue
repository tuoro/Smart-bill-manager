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

async function create() {
  creating.value = true
  error.value = ''
  try {
    const created = await api.createProvider(
      baseUrl.value,
      apiKey.value,
      model.value,
      outputMode.value,
    )
    apiKey.value = ''
    baseUrl.value = ''
    model.value = ''
    items.value.unshift(created)
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : 'AI 配置保存失败'
  } finally {
    apiKey.value = ''
    creating.value = false
  }
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
                <span>密钥指纹 {{ item.safe_fingerprint }}</span>
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
                激活
              </button>
            </div>
          </li>
        </ul>
      </section>
      <section class="panel provider-form-panel" aria-labelledby="provider-form-title">
        <div class="panel-heading">
          <div>
            <h2 id="provider-form-title">添加配置</h2>
            <p>支持兼容 OpenAI 接口的多模态模型。</p>
          </div>
        </div>
        <form class="stack-form" @submit.prevent="create">
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
              required
              aria-describedby="provider-key-note"
          /></label>
          <p id="provider-key-note" class="form-note">密钥加密保存，提交后不会在页面回显。</p>
          <button class="button button-primary button-block" type="submit" :disabled="creating">
            {{ creating ? '正在保存…' : '创建待检测配置' }}
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
