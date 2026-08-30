import { defineConfig, devices } from '@playwright/test'

const baseURL = process.env.SBM_E2E_BASE_URL ?? 'http://127.0.0.1:18084'

export default defineConfig({
  testDir: './e2e',
  outputDir: './test-results/playwright',
  fullyParallel: false,
  workers: 1,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  reporter: [['line'], ['json', { outputFile: './test-results/e2e-results.json' }]],
  use: {
    ...devices['Desktop Chrome'],
    baseURL,
    locale: 'zh-CN',
    colorScheme: 'light',
    viewport: { width: 1440, height: 900 },
    trace: 'off',
    video: 'off',
    screenshot: 'only-on-failure',
  },
})
