import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.SBM_E2E_BASE_URL ?? 'http://127.0.0.1:18084'
const outputDir = process.env.SBM_E2E_OUTPUT_DIR ?? './test-results/playwright'
const parsedBaseURL = new URL(baseURL)

if (
  parsedBaseURL.protocol !== 'http:' ||
  !['127.0.0.1', '[::1]'].includes(parsedBaseURL.hostname) ||
  parsedBaseURL.username ||
  parsedBaseURL.password ||
  parsedBaseURL.pathname !== '/' ||
  parsedBaseURL.search ||
  parsedBaseURL.hash
) {
  throw new Error('SBM_E2E_BASE_URL must be a credential-free loopback HTTP origin')
}

// 需要真实后端与真实模型服务的规格：模块顶层就读取凭据环境变量，缺少时
// 连收集都会失败。托管 CI 只跑纯合成规格，用 SBM_E2E_SYNTHETIC_ONLY 排除它们；
// 本地发布门禁不设该变量，仍然全量运行。
const backendBackedSpecs = ['**/m1-representative-flows.spec.ts']

export default defineConfig({
  testDir: './e2e',
  testIgnore: process.env.SBM_E2E_SYNTHETIC_ONLY === '1' ? backendBackedSpecs : [],
  outputDir,
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  reporter: [['line']],
  use: {
    ...devices['Desktop Chrome'],
    baseURL,
    locale: 'zh-CN',
    colorScheme: 'light',
    viewport: { width: 1440, height: 900 },
    proxy: {
      server: 'http://127.0.0.1:9',
      bypass: '127.0.0.1,localhost,[::1]',
    },
    launchOptions: {
      args: [
        '--disable-background-networking',
        '--disable-component-update',
        '--disable-domain-reliability',
      ],
    },
    trace: 'off',
    video: 'off',
    screenshot: 'only-on-failure',
  },
})
