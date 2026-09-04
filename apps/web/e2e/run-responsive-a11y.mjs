#!/usr/bin/env node

import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { cpus, platform, totalmem } from 'node:os'
import { resolve } from 'node:path'
import { pathToFileURL } from 'node:url'
import { chromium } from '@playwright/test'

import {
  SafeToolError,
  parseStrictPairs,
  readProtectedSecret,
  requireGitSHA,
  requireImageID,
  requireLoopbackURL,
  requireSHA256,
  reserveProtectedFile,
  safeErrorCode,
} from '../../../tools/lib/protected-output.mjs'

const formalWidths = [768, 1024, 1440, 1920]
const reflowCases = formalWidths.map((sourceWidth) => ({
  source_width: sourceWidth,
  width: sourceWidth / 2,
}))

async function main() {
  const options = parseArguments(process.argv.slice(2))
  const output = await reserveProtectedFile(options.output, [
    options.passwordFile,
    options.chromePath,
  ])
  let password
  let browser
  try {
    password = await readProtectedSecret(options.passwordFile, 1024)
    const client = createClient(options.server)
    await client.login(options.email, password)
    const jobs = await client.get('/jobs')
    const reviewJobs = jobs.items.filter(
      (job) => job.status === 'needs_review' && job.original_name === 'lighthouse-review.png',
    )
    if (reviewJobs.length !== 1) {
      throw new SafeToolError('synthetic_fixture_invalid')
    }
    const [reviewJob] = reviewJobs
    browser = await chromium.launch({
      executablePath: options.chromePath,
      headless: true,
      args: [
        '--no-sandbox',
        '--disable-dev-shm-usage',
        '--disable-extensions',
        '--disable-background-networking',
        '--disable-component-update',
        '--disable-domain-reliability',
        '--proxy-server=http://127.0.0.1:9',
        '--proxy-bypass-list=127.0.0.1;localhost;[::1]',
      ],
    })
    const pages = [
      { name: 'login', path: '/login', authenticated: false },
      { name: 'inbox', path: '/inbox', authenticated: true },
      {
        name: 'review',
        path: `/reviews/${encodeURIComponent(reviewJob.id)}`,
        authenticated: true,
      },
      { name: 'payments', path: '/payments', authenticated: true },
    ]
    const formal = []
    for (const width of formalWidths) {
      for (const page of pages) {
        formal.push(await measurePage(browser, client, options.server, page, width, 'formal'))
      }
    }
    const equivalentReflow = []
    for (const reflow of reflowCases) {
      for (const page of pages) {
        equivalentReflow.push(
          await measurePage(
            browser,
            client,
            options.server,
            page,
            reflow.width,
            'equivalent-200-percent',
            reflow.source_width,
          ),
        )
      }
    }
    const keyboard = await measureKeyboard(browser, options.server)
    const darkTheme = await measureDarkTheme(browser, client, options.server)
    const allResults = [...formal, ...equivalentReflow]
    const failedChecks = [
      ...allResults.filter((result) => !result.passed).map((result) => result.id),
      ...(keyboard.passed ? [] : ['keyboard']),
      ...(darkTheme.passed ? [] : ['dark-theme']),
    ]
    const report = {
      report_kind: 'm4-responsive-accessibility-result',
      measured_at: new Date().toISOString(),
      build_identity: {
        baseline_head: options.buildSha,
        release_input_sha256: options.releaseInputSha256,
        compose_config_sha256: options.composeConfigSha256,
        image_id: options.imageID,
      },
      protocol: {
        formal_widths: formalWidths,
        equivalent_reflow: reflowCases,
        equivalent_reflow_reason:
          '自动化浏览器未暴露页面缩放控制，按批准协议在相同 DPR 下将 CSS 内容宽度减半。',
        viewport_height: 1000,
        reduced_motion: 'reduce',
        network_policy: {
          loopback_origin_only: true,
          closed_loopback_proxy: true,
          background_networking_disabled: true,
        },
        pages: pages.map(({ name, path }) => ({ name, path })),
      },
      environment: {
        platform: platform(),
        architecture: process.arch,
        logical_cpu_count_visible: cpus().length,
        total_memory_bytes_visible: totalmem(),
        node: process.version,
        chrome_path: options.chromePath,
        server_origin: new URL(options.server).origin,
        script_sha256: sha256(await readFile(new URL(import.meta.url))),
      },
      fixture: { review_job_id: reviewJob.id },
      formal,
      equivalent_reflow: equivalentReflow,
      keyboard,
      dark_theme: darkTheme,
      failed_checks: failedChecks,
      passed: failedChecks.length === 0,
    }
    const encoded = JSON.stringify(report)
    assertNoSecret(encoded, [password, ...client.cookieSecrets()])
    await output.writeJSON(report)
    process.stdout.write(
      `${JSON.stringify({
        report_kind: report.report_kind,
        formal_passed: formal.filter((result) => result.passed).length,
        formal_total: formal.length,
        equivalent_reflow_passed: equivalentReflow.filter((result) => result.passed).length,
        equivalent_reflow_total: equivalentReflow.length,
        keyboard_passed: keyboard.passed,
        dark_theme_passed: darkTheme.passed,
        passed: report.passed,
      })}\n`,
    )
    if (!report.passed) process.exitCode = 1
  } finally {
    password?.fill(0)
    try {
      await browser?.close()
    } finally {
      await output.close()
    }
  }
}

async function measurePage(browser, client, server, descriptor, width, mode, sourceWidth) {
  const context = await browser.newContext({
    viewport: { width, height: 1000 },
    locale: 'zh-CN',
    colorScheme: 'light',
    reducedMotion: 'reduce',
  })
  const errors = []
  const expectedMessages = []
  try {
    if (descriptor.authenticated) await context.addCookies(client.cookies())
    const page = await context.newPage()
    page.on('pageerror', (error) => errors.push(`pageerror: ${error.message}`))
    page.on('console', (message) => {
      if (message.type() !== 'error') return
      const rendered = `console: ${message.text()}`
      if (descriptor.name === 'login' && message.text().includes('401 (Unauthorized)')) {
        expectedMessages.push(rendered)
        return
      }
      errors.push(rendered)
    })
    const target = new URL(descriptor.path, ensureTrailingSlash(server)).toString()
    const response = await page.goto(target, { waitUntil: 'networkidle', timeout: 30_000 })
    if (!response?.ok())
      throw new Error(`${descriptor.name} ${width}px returned HTTP ${response?.status()}`)
    const finalURL = new URL(page.url())
    const expectedURL = new URL(descriptor.path, ensureTrailingSlash(server))
    if (finalURL.origin !== expectedURL.origin || finalURL.pathname !== expectedURL.pathname) {
      throw new Error(`${descriptor.name} ${width}px ended outside its expected local route`)
    }
    await page.locator('#main-content').waitFor({ state: 'visible', timeout: 15_000 })
    const metrics = await page.evaluate(() => {
      const root = document.documentElement
      const main = document.querySelector('#main-content')
      const reviewGrid = document.querySelector('.review-grid')
      const sidebar = document.querySelector('.sidebar')
      const visible = (element) => {
        const style = getComputedStyle(element)
        const rect = element.getBoundingClientRect()
        return (
          style.display !== 'none' &&
          style.visibility !== 'hidden' &&
          rect.width > 0 &&
          rect.height > 0
        )
      }
      const durations = (value) =>
        value.split(',').map((entry) => {
          const trimmed = entry.trim()
          return trimmed.endsWith('ms')
            ? Number.parseFloat(trimmed)
            : Number.parseFloat(trimmed) * 1000
        })
      const visibleElements = [...document.querySelectorAll('body *')].filter(visible)
      const motionViolations = visibleElements.filter((element) => {
        const style = getComputedStyle(element)
        return (
          Math.max(...durations(style.animationDuration), 0) > 0.02 ||
          Math.max(...durations(style.transitionDuration), 0) > 0.02 ||
          Number.parseFloat(style.animationIterationCount) > 1
        )
      })
      const focusableOutsideViewport = [
        ...document.querySelectorAll('a[href], button, input, select, textarea, [tabindex]'),
      ].filter((element) => {
        if (!visible(element)) return false
        if (element.classList.contains('visually-hidden') && element.closest('label')) return false
        let ancestor = element.parentElement
        while (ancestor) {
          const style = getComputedStyle(ancestor)
          if (
            (style.overflowX === 'auto' || style.overflowX === 'scroll') &&
            ancestor.scrollWidth > ancestor.clientWidth
          ) {
            return false
          }
          ancestor = ancestor.parentElement
        }
        const rect = element.getBoundingClientRect()
        return rect.left < -0.5 || rect.right > root.clientWidth + 0.5
      })
      const unlabeledFormControls = [
        ...document.querySelectorAll('input:not([type="hidden"]), select, textarea'),
      ].filter(
        (element) =>
          visible(element) &&
          !element.labels?.length &&
          !element.getAttribute('aria-label') &&
          !element.getAttribute('aria-labelledby'),
      )
      const mainStyle = main ? getComputedStyle(main) : null
      const reviewStyle = reviewGrid ? getComputedStyle(reviewGrid) : null
      return {
        viewport: { width: window.innerWidth, height: window.innerHeight },
        root_width: { client: root.clientWidth, scroll: root.scrollWidth },
        root_height: { client: root.clientHeight, scroll: root.scrollHeight },
        main_count: document.querySelectorAll('main').length,
        main_visible: Boolean(main && visible(main)),
        main_padding_left: mainStyle ? Number.parseFloat(mainStyle.paddingLeft) : null,
        heading: main?.querySelector('h1, h2')?.textContent?.trim() ?? '',
        sidebar_width: sidebar ? sidebar.getBoundingClientRect().width : 0,
        review_columns:
          reviewStyle?.display === 'grid'
            ? reviewStyle.gridTemplateColumns.split(' ').filter(Boolean).length
            : null,
        focusable_outside_viewport: focusableOutsideViewport.length,
        unlabeled_form_controls: unlabeledFormControls.length,
        reduced_motion_matches: matchMedia('(prefers-reduced-motion: reduce)').matches,
        motion_violations: motionViolations.length,
      }
    })
    const failures = []
    if (metrics.root_width.scroll > metrics.root_width.client + 1)
      failures.push('root-horizontal-overflow')
    if (metrics.main_count !== 1 || !metrics.main_visible) failures.push('main-landmark')
    if (!metrics.heading) failures.push('missing-heading')
    if (metrics.focusable_outside_viewport !== 0) failures.push('focusable-clipping')
    if (metrics.unlabeled_form_controls !== 0) failures.push('unlabeled-form-control')
    if (!metrics.reduced_motion_matches || metrics.motion_violations !== 0) {
      failures.push('reduced-motion')
    }
    if (errors.length) failures.push('runtime-error')
    if (descriptor.authenticated) {
      const expectedPadding = width < 768 ? 12 : width < 1024 ? 16 : 24
      if (Math.abs(metrics.main_padding_left - expectedPadding) > 0.1) failures.push('main-padding')
    }
    if (descriptor.name === 'review') {
      const expectedColumns = width < 1024 ? 1 : width < 1440 ? 2 : 3
      if (metrics.review_columns !== expectedColumns) failures.push('review-columns')
    }
    return {
      id: `${mode}:${sourceWidth ? `${sourceWidth}-to-` : ''}${width}:${descriptor.name}`,
      mode,
      source_width: sourceWidth,
      width,
      page: descriptor.name,
      path: descriptor.path,
      metrics,
      expected_browser_messages: expectedMessages,
      browser_errors: errors,
      failures,
      passed: failures.length === 0,
    }
  } finally {
    await context.close()
  }
}

async function measureKeyboard(browser, server) {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    locale: 'zh-CN',
    colorScheme: 'light',
    reducedMotion: 'reduce',
  })
  try {
    const page = await context.newPage()
    await page.goto(new URL('/login', ensureTrailingSlash(server)).toString(), {
      waitUntil: 'networkidle',
      timeout: 30_000,
    })
    await page.keyboard.press('Tab')
    await page.keyboard.press('Enter')
    await page.waitForTimeout(50)
    const skipResult = await page.evaluate(() => ({
      hash: location.hash,
      active_id: document.activeElement?.id ?? '',
    }))
    await page.goto(new URL('/login', ensureTrailingSlash(server)).toString(), {
      waitUntil: 'networkidle',
      timeout: 30_000,
    })
    const focusSequence = []
    const expectedSequence = [
      'skip-link',
      'brand',
      '切换到深色模式',
      'email',
      'password',
      'password-toggle',
      'login-submit',
    ]
    for (let index = 0; index < expectedSequence.length; index += 1) {
      await page.keyboard.press('Tab')
      focusSequence.push(
        await page.evaluate(() => {
          const element = document.activeElement
          if (!(element instanceof HTMLElement)) return { signature: '', outline: '' }
          const style = getComputedStyle(element)
          const signature = element.classList.contains('skip-link')
            ? 'skip-link'
            : element.classList.contains('brand')
              ? 'brand'
              : element.getAttribute('name') ||
                (element.classList.contains('password-toggle')
                  ? 'password-toggle'
                  : element.classList.contains('login-submit')
                    ? 'login-submit'
                    : element.getAttribute('aria-label') || element.tagName.toLowerCase())
          return {
            signature,
            outline: `${style.outlineStyle} ${style.outlineWidth}`,
          }
        }),
      )
    }
    await page.getByRole('button', { name: '显示密码', exact: true }).click()
    const passwordToggle = await page.evaluate(() => ({
      input_type: document.querySelector('input[name="password"]')?.getAttribute('type'),
      button_name: document.querySelector('.password-toggle')?.getAttribute('aria-label'),
    }))
    await page.getByLabel('邮箱', { exact: true }).fill('missing-user@example.test')
    await page.getByLabel('密码', { exact: true }).fill('definitely-wrong-password')
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await page.getByRole('alert').waitFor({ state: 'visible', timeout: 15_000 })
    const errorBinding = await page.evaluate(() => ({
      alert_id: document.querySelector('[role="alert"]')?.id ?? '',
      email_described_by: document
        .querySelector('input[name="email"]')
        ?.getAttribute('aria-describedby'),
      password_described_by: document
        .querySelector('input[name="password"]')
        ?.getAttribute('aria-describedby'),
    }))
    const failures = []
    if (skipResult.hash !== '#main-content' || skipResult.active_id !== 'main-content') {
      failures.push('skip-link-target')
    }
    if (focusSequence.map((entry) => entry.signature).join('|') !== expectedSequence.join('|')) {
      failures.push('focus-order')
    }
    if (
      focusSequence.some(
        (entry) => entry.outline.startsWith('none') || entry.outline.endsWith(' 0px'),
      )
    ) {
      failures.push('focus-visibility')
    }
    if (passwordToggle.input_type !== 'text' || passwordToggle.button_name !== '隐藏密码') {
      failures.push('password-toggle')
    }
    if (
      !errorBinding.alert_id ||
      errorBinding.email_described_by !== errorBinding.alert_id ||
      errorBinding.password_described_by !== errorBinding.alert_id
    ) {
      failures.push('error-binding')
    }
    return {
      skip_link: skipResult,
      expected_focus_sequence: expectedSequence,
      focus_sequence: focusSequence,
      password_toggle: passwordToggle,
      error_binding: errorBinding,
      failures,
      passed: failures.length === 0,
    }
  } finally {
    await context.close()
  }
}

async function measureDarkTheme(browser, client, server) {
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    locale: 'zh-CN',
    colorScheme: 'light',
    reducedMotion: 'reduce',
  })
  try {
    await context.addCookies(client.cookies())
    const page = await context.newPage()
    await page.goto(new URL('/inbox', ensureTrailingSlash(server)).toString(), {
      waitUntil: 'networkidle',
      timeout: 30_000,
    })
    await page.locator('input[type="file"]').focus()
    const uploadFocus = await page.locator('.upload-button').evaluate((element) => {
      const style = getComputedStyle(element)
      return { outline: `${style.outlineStyle} ${style.outlineWidth}` }
    })
    await page.getByRole('button', { name: '切换到深色模式', exact: true }).click()
    const state = await page
      .getByRole('button', { name: '切换到浅色模式', exact: true })
      .evaluate((button) => ({
        theme: document.documentElement.dataset.theme,
        stored: localStorage.getItem('sbm_theme'),
        control_name: button.getAttribute('aria-label'),
      }))
    const failures = []
    if (uploadFocus.outline.startsWith('none') || uploadFocus.outline.endsWith(' 0px')) {
      failures.push('upload-focus-visibility')
    }
    if (
      state.theme !== 'dark' ||
      state.stored !== 'dark' ||
      state.control_name !== '切换到浅色模式'
    ) {
      failures.push('dark-theme-state')
    }
    return { upload_focus: uploadFocus, state, failures, passed: failures.length === 0 }
  } finally {
    await context.close()
  }
}

function createClient(server) {
  const base = new URL('/api/v1/', ensureTrailingSlash(server)).toString().replace(/\/$/, '')
  let cookieHeader = ''
  let cookieValues = []
  return {
    async login(email, passwordBytes) {
      const response = await fetch(base + '/session/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password: passwordBytes.toString('utf8') }),
        signal: AbortSignal.timeout(30_000),
        redirect: 'error',
      })
      if (!response.ok) throw new Error(`login: HTTP ${response.status}`)
      const pairs = response.headers.getSetCookie().map((entry) => entry.split(';', 1)[0])
      cookieValues = pairs.map((pair) => {
        const separator = pair.indexOf('=')
        if (separator < 1) throw new Error('login returned an invalid session cookie')
        return {
          name: pair.slice(0, separator),
          value: pair.slice(separator + 1),
          url: new URL(server).origin,
        }
      })
      if (cookieValues.length < 2)
        throw new Error('login did not establish session and CSRF cookies')
      cookieHeader = pairs.join('; ')
    },
    async get(path) {
      const response = await fetch(base + path, {
        headers: { Cookie: cookieHeader },
        signal: AbortSignal.timeout(30_000),
        redirect: 'error',
      })
      if (!response.ok) throw new Error(`${path}: HTTP ${response.status}`)
      return response.json()
    },
    cookies() {
      if (!cookieValues.length) throw new Error('session cookies are unavailable')
      return cookieValues
    },
    cookieSecrets() {
      return cookieSecretBuffers(cookieHeader)
    },
  }
}

export function parseArguments(argumentsList) {
  const values = parseStrictPairs(argumentsList, [
    'server',
    'email',
    'password-file',
    'chrome-path',
    'output',
    'build-sha',
    'release-input-sha256',
    'compose-config-sha256',
    'image-id',
  ])
  requireLoopbackURL(values.get('server'), { allowPath: false })
  if (!/^[^\s@]+@[^\s@]+$/.test(values.get('email'))) {
    throw new SafeToolError('invalid_arguments')
  }
  return {
    server: values.get('server'),
    email: values.get('email'),
    passwordFile: resolve(values.get('password-file')),
    chromePath: resolve(values.get('chrome-path')),
    output: resolve(values.get('output')),
    buildSha: requireGitSHA(values.get('build-sha')),
    releaseInputSha256: requireSHA256(values.get('release-input-sha256')),
    composeConfigSha256: requireSHA256(values.get('compose-config-sha256')),
    imageID: requireImageID(values.get('image-id')),
  }
}

function assertNoSecret(encoded, secrets) {
  for (const secret of secrets) {
    if (secret.length && encoded.includes(secret.toString('utf8'))) {
      throw new Error('refusing to write responsive output containing a secret')
    }
  }
}

function cookieSecretBuffers(cookieHeader) {
  return cookieHeader.split(/;\s*/).flatMap((pair) => {
    const separator = pair.indexOf('=')
    return separator < 1
      ? []
      : [Buffer.from(pair, 'utf8'), Buffer.from(pair.slice(separator + 1), 'utf8')]
  })
}

function sha256(content) {
  return createHash('sha256').update(content).digest('hex')
}

function ensureTrailingSlash(value) {
  return value.endsWith('/') ? value : `${value}/`
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`responsive-a11y: ${safeErrorCode(error)}\n`)
    process.exitCode = 1
  })
}
