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

export default defineConfig({
  testDir: './e2e',
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
