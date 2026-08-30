#!/usr/bin/env node

import { createHash } from 'node:crypto'
import { mkdir, readFile, stat, writeFile } from 'node:fs/promises'
import { cpus, platform, totalmem } from 'node:os'
import { dirname, resolve } from 'node:path'
import { launch } from 'chrome-launcher'
import lighthouse, { desktopConfig } from 'lighthouse'

const runsPerPage = 3
const thresholds = { accessibility: 95, performance: 85 }

async function main() {
  const options = parseArguments(process.argv.slice(2))
  const password = await readProtectedSecret(options.passwordFile, 1024)
  const client = createClient(options.server)
  try {
    await client.login(options.email, password)
    const reviewJobID = await ensureReviewFixture(client, options.source)
    await createOutputDirectory(options.output)
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
          assertFinalURL(page, result.lhr.finalDisplayedUrl)
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
          const encoded = `${JSON.stringify(sanitized, null, 2)}\n`
          assertNoSecret(encoded, [password, Buffer.from(client.cookie())])
          await writeFile(resolve(options.output, `${page.name}-run-${run}.json`), encoded, {
            encoding: 'utf8',
            flag: 'wx',
            mode: 0o600,
          })
          process.stderr.write(
            `${page.name} ${run}/${runsPerPage}: performance=${scores.performance} accessibility=${scores.accessibility}\n`,
          )
        } finally {
          chrome.kill()
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
      report_kind: 'm1-lighthouse-result',
      measured_at: new Date().toISOString(),
      lighthouse_version: lighthouseVersion,
      protocol: {
        runs_per_page: runsPerPage,
        preset: 'desktop',
        browser_profile: 'fresh temporary profile per page run',
        extensions: 'disabled',
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
    const encoded = `${JSON.stringify(summary, null, 2)}\n`
    assertNoSecret(encoded, [password, Buffer.from(client.cookie())])
    await writeFile(resolve(options.output, 'summary.json'), encoded, {
      encoding: 'utf8',
      flag: 'wx',
      mode: 0o600,
    })
    process.stdout.write(encoded)
    if (failedPages.length) process.exitCode = 1
  } finally {
    password.fill(0)
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
  const marker = Buffer.from('SBM-M1-LIGHTHOUSE-REVIEW', 'utf8')
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
  }
}

function assertFinalURL(page, finalURL) {
  const actual = new URL(finalURL)
  if (actual.pathname !== page.path) {
    throw new Error(`${page.name} ended at ${actual.pathname}, want ${page.path}`)
  }
}

function score(value) {
  if (typeof value !== 'number') throw new Error('Lighthouse category score is missing')
  return Math.round(value * 100)
}

async function createOutputDirectory(path) {
  const parent = dirname(path)
  const parentInformation = await stat(parent)
  if (!parentInformation.isDirectory())
    throw new Error('Lighthouse output parent is not a directory')
  await mkdir(path, { mode: 0o700 })
}

function parseArguments(argumentsList) {
  const values = new Map()
  for (let index = 0; index < argumentsList.length; index += 2) {
    const key = argumentsList[index]
    const value = argumentsList[index + 1]
    if (!key?.startsWith('--') || value === undefined)
      throw new Error(`invalid argument near ${key ?? '<end>'}`)
    values.set(key.slice(2), value)
  }
  for (const name of ['server', 'email', 'password-file', 'source', 'chrome-path', 'output']) {
    if (!values.get(name)) throw new Error(`--${name} is required`)
  }
  return {
    server: values.get('server'),
    email: values.get('email'),
    passwordFile: resolve(values.get('password-file')),
    source: resolve(values.get('source')),
    chromePath: resolve(values.get('chrome-path')),
    output: resolve(values.get('output')),
  }
}

async function readProtectedSecret(path, maximumBytes) {
  const information = await stat(path)
  if (!information.isFile() || (information.mode & 0o077) !== 0) {
    throw new Error('password file must be regular and owner-only')
  }
  const content = await readFile(path)
  const end =
    content.at(-1) === 0x0a ? content.length - (content.at(-2) === 0x0d ? 2 : 1) : content.length
  const result = Buffer.from(content.subarray(0, end))
  if (result.length < 1 || result.length > maximumBytes)
    throw new Error('password file size is invalid')
  return result
}

function assertNoSecret(encoded, secrets) {
  for (const secret of secrets) {
    if (secret.length && encoded.includes(secret.toString('utf8'))) {
      throw new Error('refusing to write Lighthouse output containing a secret')
    }
  }
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

main().catch((error) => {
  process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`)
  process.exitCode = 1
})
