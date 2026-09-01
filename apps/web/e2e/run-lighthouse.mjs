#!/usr/bin/env node

import { createHash } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { cpus, platform, totalmem } from 'node:os'
import { dirname, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { launch } from 'chrome-launcher'
import lighthouse, { desktopConfig } from 'lighthouse'

import {
  SafeToolError,
  parseStrictPairs,
  readProtectedSecret,
  requireGitSHA,
  requireImageID,
  requireLoopbackURL,
  requireSHA256,
  reserveProtectedDirectory,
  safeErrorCode,
  writeProtectedChild,
} from '../../../tools/lib/protected-output.mjs'

const runsPerPage = 3
const thresholds = { accessibility: 95, performance: 85 }
const currentFile = fileURLToPath(import.meta.url)
const expectedSource = resolve(
  dirname(currentFile),
  '../../../tests/evaluation/assets/m1-synthetic-v1/pay-001.png',
)

async function main() {
  const options = parseArguments(process.argv.slice(2))
  const output = await reserveProtectedDirectory(options.output, [
    options.passwordFile,
    options.source,
    options.chromePath,
  ])
  let password
  try {
    password = await readProtectedSecret(options.passwordFile, 1024)
    const client = createClient(options.server)
    await client.login(options.email, password)
    const reviewJobID = await ensureReviewFixture(client, options.source)
    const pages = [
      { name: 'login', path: '/login', authenticated: false },
      { name: 'inbox', path: '/inbox', authenticated: true },
      { name: 'review', path: `/reviews/${encodeURIComponent(reviewJobID)}`, authenticated: true },
      { name: 'payments', path: '/payments', authenticated: true },
    ]
    const summaries = {}
    let lighthouseVersion = ''
    for (const page of pages) {
      const runs = []
      for (let run = 1; run <= runsPerPage; run += 1) {
        const url = new URL(page.path, ensureTrailingSlash(options.server)).toString()
        const chrome = await launch({
          chromePath: options.chromePath,
          chromeFlags: [
            '--headless=new',
            '--no-sandbox',
            '--disable-dev-shm-usage',
            '--disable-extensions',
            '--disable-background-networking',
            '--disable-component-update',
            '--disable-domain-reliability',
            '--proxy-server=http://127.0.0.1:9',
            '--proxy-bypass-list=127.0.0.1;localhost;[::1]',
          ],
          handleSIGINT: false,
          logLevel: 'silent',
        })
        try {
          const result = await lighthouse(
            url,
            {
              port: chrome.port,
              logLevel: 'error',
              output: 'json',
              onlyCategories: ['performance', 'accessibility'],
              extraHeaders: page.authenticated ? { Cookie: client.cookie() } : undefined,
            },
            desktopConfig,
          )
          if (!result) throw new Error(`${page.name} run ${run} returned no Lighthouse result`)
          lighthouseVersion = result.lhr.lighthouseVersion
          assertFinalURL(page, result.lhr.finalDisplayedUrl, options.server)
          const scores = {
            run,
            performance: score(result.lhr.categories.performance.score),
            accessibility: score(result.lhr.categories.accessibility.score),
            fetch_time: result.lhr.fetchTime,
          }
          runs.push(scores)
          const sanitized = structuredClone(result.lhr)
          if (sanitized.configSettings.extraHeaders) {
            sanitized.configSettings.extraHeaders = { Cookie: '[REDACTED]' }
          }
          const encoded = JSON.stringify(sanitized)
          assertNoSecret(encoded, [password, ...client.cookieSecrets()])
          await writeProtectedChild(output, `${page.name}-run-${run}.json`, sanitized)
          process.stderr.write(
            `${page.name} ${run}/${runsPerPage}: performance=${scores.performance} accessibility=${scores.accessibility}\n`,
          )
        } finally {
          await chrome.kill()
        }
      }
      const worstPerformance = Math.min(...runs.map((run) => run.performance))
      const worstAccessibility = Math.min(...runs.map((run) => run.accessibility))
      summaries[page.name] = {
        path: page.path,
        authenticated: page.authenticated,
        runs,
        worst_performance: worstPerformance,
        worst_accessibility: worstAccessibility,
        performance_threshold: thresholds.performance,
        accessibility_threshold: thresholds.accessibility,
        passed:
          worstPerformance >= thresholds.performance &&
          worstAccessibility >= thresholds.accessibility,
      }
    }
    const failedPages = Object.entries(summaries)
      .filter(([, page]) => !page.passed)
      .map(([name]) => name)
    const summary = {
      report_kind: 'm4-lighthouse-result',
      measured_at: new Date().toISOString(),
      build_identity: {
        baseline_head: options.buildSha,
        release_input_sha256: options.releaseInputSha256,
        compose_config_sha256: options.composeConfigSha256,
        image_id: options.imageID,
      },
      lighthouse_version: lighthouseVersion,
      protocol: {
        runs_per_page: runsPerPage,
        preset: 'desktop',
        browser_profile: 'fresh temporary profile per page run',
        extensions: 'disabled',
        network_policy: {
          loopback_origin_only: true,
          closed_loopback_proxy: true,
          background_networking_disabled: true,
        },
        categories: ['performance', 'accessibility'],
        worst_run_decides: true,
      },
      environment: {
        platform: platform(),
        architecture: process.arch,
        logical_cpu_count_visible: cpus().length,
        total_memory_bytes_visible: totalmem(),
        node: process.version,
        chrome_path: options.chromePath,
        server_origin: new URL(options.server).origin,
      },
      fixture: {
        review_job_id: reviewJobID,
        synthetic_source_sha256: sha256(await readFile(options.source)),
      },
      pages: summaries,
      failed_pages: failedPages,
      passed: failedPages.length === 0,
    }
    const encoded = JSON.stringify(summary)
    assertNoSecret(encoded, [password, ...client.cookieSecrets()])
    await writeProtectedChild(output, 'summary.json', summary)
    process.stdout.write(
      `${JSON.stringify({
        report_kind: summary.report_kind,
        page_count: Object.keys(summaries).length,
        runs_per_page: runsPerPage,
        minimum_performance: Math.min(
          ...Object.values(summaries).map((page) => page.worst_performance),
        ),
        minimum_accessibility: Math.min(
          ...Object.values(summaries).map((page) => page.worst_accessibility),
        ),
        passed: summary.passed,
      })}\n`,
    )
    if (failedPages.length) process.exitCode = 1
  } finally {
    password?.fill(0)
  }
}

async function ensureReviewFixture(client, sourcePath) {
  const jobs = await client.get('/jobs')
  const reusable = jobs.items.find(
    (job) => job.original_name === 'lighthouse-review.png' && job.status === 'needs_review',
  )
  if (reusable) return reusable.id
  if (jobs.items.some((job) => job.original_name === 'lighthouse-review.png')) {
    throw new Error('lighthouse review fixture already exists in a non-reviewable state')
  }
  const source = await readFile(sourcePath)
  const marker = Buffer.from('SBM-M4-LIGHTHOUSE-REVIEW', 'utf8')
  const uploaded = await client.upload('lighthouse-review.png', Buffer.concat([source, marker]))
  const deadline = Date.now() + 30_000
  while (Date.now() < deadline) {
    const job = await client.get(`/jobs/${encodeURIComponent(uploaded.job_id)}`)
    if (job.status === 'needs_review') return job.id
    if (['blocked', 'failed', 'cancelled', 'completed', 'rejected'].includes(job.status)) {
      throw new Error(`lighthouse review fixture stopped at ${job.status}`)
    }
    await delay(200)
  }
  throw new Error('lighthouse review fixture did not become reviewable within 30 seconds')
}

function createClient(server) {
  const base = new URL('/api/v1/', ensureTrailingSlash(server)).toString().replace(/\/$/, '')
  let cookie = ''
  let csrf = ''
  async function request(path, options = {}) {
    const headers = new Headers(options.headers)
    if (cookie) headers.set('Cookie', cookie)
    if (options.csrf) headers.set('X-CSRF-Token', csrf)
    const response = await fetch(base + path, {
      method: options.method ?? 'GET',
      headers,
      body: options.body,
      signal: AbortSignal.timeout(options.timeoutMs ?? 30_000),
      redirect: 'error',
    })
    if (!response.ok) throw new Error(`${path}: HTTP ${response.status}`)
    return response.status === 204 ? undefined : response.json()
  }
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
      cookie = response.headers
        .getSetCookie()
        .map((entry) => entry.split(';', 1)[0])
        .join('; ')
      const body = await response.json()
      csrf = body.csrf_token
      if (!cookie || !csrf) throw new Error('login did not establish session and CSRF state')
    },
    get(path) {
      return request(path)
    },
    async upload(name, content) {
      const form = new FormData()
      form.append('file', new Blob([content], { type: 'image/png' }), name)
      return request('/documents', { method: 'POST', csrf: true, body: form, timeoutMs: 60_000 })
    },
    cookie() {
      return cookie
    },
    cookieSecrets() {
      return cookieSecretBuffers(cookie)
    },
  }
}

function assertFinalURL(page, finalURL, server) {
  const actual = new URL(finalURL)
  const expected = new URL(page.path, ensureTrailingSlash(server))
  if (actual.origin !== expected.origin || actual.pathname !== expected.pathname) {
    throw new Error(`${page.name} ended outside its expected local route`)
  }
}

function score(value) {
  if (typeof value !== 'number') throw new Error('Lighthouse category score is missing')
  return Math.round(value * 100)
}

export function parseArguments(argumentsList) {
  const values = parseStrictPairs(argumentsList, [
    'server',
    'email',
    'password-file',
    'source',
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
  const result = {
    server: values.get('server'),
    email: values.get('email'),
    passwordFile: resolve(values.get('password-file')),
    source: resolve(values.get('source')),
    chromePath: resolve(values.get('chrome-path')),
    output: resolve(values.get('output')),
    buildSha: requireGitSHA(values.get('build-sha')),
    releaseInputSha256: requireSHA256(values.get('release-input-sha256')),
    composeConfigSha256: requireSHA256(values.get('compose-config-sha256')),
    imageID: requireImageID(values.get('image-id')),
  }
  if (result.source !== expectedSource) {
    throw new SafeToolError('synthetic_source_required')
  }
  return result
}

function assertNoSecret(encoded, secrets) {
  for (const secret of secrets) {
    if (secret.length && encoded.includes(secret.toString('utf8'))) {
      throw new Error('refusing to write Lighthouse output containing a secret')
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

function delay(milliseconds) {
  return new Promise((resolveDelay) => setTimeout(resolveDelay, milliseconds))
}

if (pathToFileURL(process.argv[1]).href === import.meta.url) {
  main().catch((error) => {
    process.stderr.write(`lighthouse: ${safeErrorCode(error)}\n`)
    process.exitCode = 1
  })
}
