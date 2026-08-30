<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ApiError, api, type ProviderConfig } from '../../data/client'

const items = ref<ProviderConfig[]>([])
const loading = ref(true)
const error = ref('')
const actionId = ref('')
const creating = ref(false)
const baseUrl = ref('')
const model = ref('')
const apiKey = ref('')
const outputMode = ref<ProviderConfig['output_mode']>('json_schema')

async function load() {
  loading.value = true
  try {
    items.value = (await api.providerConfigs()).items
    error.value = ''
  } catch (caught) {
    error.value = caught instanceof ApiError ? caught.message : 'Provider 配置加载失败'
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
    error.value = caught instanceof ApiError ? caught.message : 'Provider 配置创建失败'
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
    error.value = caught instanceof ApiError ? caught.message : '激活配置失败'
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
        <h1>AI Provider</h1>
        <p>只有通过图片输入、所选输出模式与本地 Schema 检测的配置才能激活。</p>
      </div>
    </header>
    <div v-if="error" class="notice notice-danger" role="alert">
      <span aria-hidden="true">!</span><span>{{ error }}</span>
    </div>
    <div class="settings-grid">
      <section class="panel" aria-labelledby="provider-list-title">
        <div class="panel-heading">
          <div>
            <h2 id="provider-list-title">配置版本</h2>
            <p>列表不会返回可恢复密钥材料</p>
          </div>
        </div>
        <div v-if="loading" class="state-layout" role="status">
          <span class="spinner spinner-large" aria-hidden="true"></span
          ><strong>正在读取配置</strong>
        </div>
        <div v-else-if="items.length === 0" class="state-layout compact">
          <span class="state-glyph" aria-hidden="true">配</span
          ><strong>还没有 ProviderConfig</strong><span>先在右侧创建一个待检测版本。</span>
        </div>
        <ul v-else class="provider-list">
          <li v-for="item in items" :key="item.id">
            <div class="provider-main">
              <div>
                <strong>{{ item.model }}</strong
                ><span
                  class="status"
                  :data-tone="
                    item.active
                      ? 'success'
                      : item.capability_status === 'failed'
                        ? 'danger'
                        : item.capability_status === 'passed'
                          ? 'info'
                          : 'neutral'
                  "
                  ><span aria-hidden="true">●</span
                  >{{ item.active ? '活动配置' : item.capability_status }}</span
                >
              </div>
              <p>{{ item.base_url }}</p>
              <small
                >v{{ item.version }} · {{ item.output_mode }} · 指纹
                {{ item.safe_fingerprint }}</small
              ><small v-if="item.capability_safe_message">{{ item.capability_safe_message }}</small>
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
            <h2 id="provider-form-title">创建配置</h2>
            <p>Base URL + API Key + Model + Output Mode 形成独立版本</p>
          </div>
        </div>
        <form class="stack-form" @submit.prevent="create">
          <label class="field-stack"
            ><span>Base URL</span
            ><input
              v-model.trim="baseUrl"
              class="input"
              type="url"
              placeholder="https://provider.example/v1"
              maxlength="2048"
              required /></label
          ><label class="field-stack"
            ><span>Model</span
            ><input
              v-model.trim="model"
              class="input"
              type="text"
              maxlength="200"
              autocomplete="off"
              required /></label
          ><label class="field-stack"
            ><span>Output Mode</span
            ><select v-model="outputMode" class="select" aria-describedby="provider-mode-note">
              <option value="json_schema">json_schema（Provider 严格 Schema）</option>
              <option value="json_object">json_object（本地严格 Schema）</option>
            </select></label
          >
          <p id="provider-mode-note" class="form-note">
            按 Provider 官方能力显式选择；系统不会自动降级或切换模式。
          </p>
          <label class="field-stack"
            ><span>API Key</span
            ><input
              v-model="apiKey"
              class="input"
              type="password"
              maxlength="4096"
              autocomplete="off"
              required
              aria-describedby="provider-key-note"
          /></label>
          <p id="provider-key-note" class="form-note">
            密钥会在提交边界加密；页面不会回显或持久化明文。
          </p>
          <button class="button button-primary button-block" type="submit" :disabled="creating">
            {{ creating ? '正在加密保存…' : '创建待检测配置' }}
          </button>
        </form>
      </section>
    </div>
  </div>
</template>
