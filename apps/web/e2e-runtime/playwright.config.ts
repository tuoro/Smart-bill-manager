import { defineConfig, devices } from '@playwright/test'
import { lstatSync, realpathSync } from 'node:fs'
import { isAbsolute, resolve } from 'node:path'
import process from 'node:process'

function requiredEnvironment(name: string): string {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is required for the isolated throughflow test`)
  return value
}

const baseURL = requiredEnvironment('SBM_THROUGHFLOW_URL')
const origin = new URL(baseURL)
if (
  origin.protocol !== 'http:' ||
  origin.hostname !== '127.0.0.1' ||
  !origin.port ||
  Number(origin.port) < 1024 ||
  origin.port === '19086' ||
  origin.username ||
  origin.password ||
  origin.pathname !== '/' ||
  origin.search ||
  origin.hash
) {
  throw new Error('SBM_THROUGHFLOW_URL must be an explicit loopback HTTP application origin')
}

const outputDirectory = requiredEnvironment('SBM_THROUGHFLOW_OUTPUT_DIR')
if (!isAbsolute(outputDirectory)) throw new Error('throughflow output directory must be absolute')
const outputInformation = lstatSync(outputDirectory)
if (
  !outputInformation.isDirectory() ||
  outputInformation.isSymbolicLink() ||
  outputInformation.uid !== process.getuid?.() ||
  (outputInformation.mode & 0o077) !== 0 ||
  realpathSync(outputDirectory) !== resolve(outputDirectory)
) {
  throw new Error('throughflow output must be an existing owner-only private directory')
}

export const throughflowEnvironment = {
  origin: origin.origin,
  ownerEmail: requiredEnvironment('SBM_THROUGHFLOW_OWNER_EMAIL'),
  ownerPasswordFile: requiredEnvironment('SBM_THROUGHFLOW_OWNER_PASSWORD_FILE'),
  providerKeyFile: requiredEnvironment('SBM_THROUGHFLOW_PROVIDER_KEY_FILE'),
  model: requiredEnvironment('SBM_THROUGHFLOW_MODEL'),
  outputDirectory: resolve(outputDirectory),
  providerBaseURL: 'http://127.0.0.1:19086/v1',
}
if (!/^synthetic-[a-z0-9._-]+$/.test(throughflowEnvironment.model)) {
  throw new Error('throughflow provider model must use a synthetic identity')
}

// 失败时也不自动采集可能包含登录信息的页面快照。
process.env.PLAYWRIGHT_NO_COPY_PROMPT = '1'

export default defineConfig({
  testDir: '.',
  testMatch: 'review-allocation.spec.ts',
  outputDir: resolve(outputDirectory, 'playwright-artifacts'),
  fullyParallel: false,
  workers: 1,
  forbidOnly: true,
  retries: 0,
  timeout: 240_000,
  expect: { timeout: 15_000 },
  reporter: [['line']],
  use: {
    ...devices['Desktop Chrome'],
    baseURL: origin.origin,
    locale: 'zh-CN',
    colorScheme: 'light',
    viewport: { width: 1440, height: 1000 },
    serviceWorkers: 'block',
    proxy: { server: 'http://127.0.0.1:9', bypass: '127.0.0.1' },
    launchOptions: {
      args: [
        '--disable-background-networking',
        '--disable-component-update',
        '--disable-domain-reliability',
      ],
    },
    trace: 'off',
    video: 'off',
    screenshot: 'off',
  },
})
