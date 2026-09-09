import { expect, test, type Page } from '@playwright/test'
import {
  mockSuggestionWorkspace,
  suggestionScenarios,
  type SuggestionScenario,
} from './allocation-suggestion-fixtures'
import { captureResponsiveReview } from './visual-review'

const checks = new WeakMap<Page, { errors: string[]; unexpected: string[] }>()
const path = (scenario: SuggestionScenario) =>
  `/allocations/${scenario.workspace.anchor.fact_type}/${scenario.workspace.anchor.id}`
const workspaceAPI = (scenario: SuggestionScenario) => `**/api/v1${path(scenario)}`
const adopt = (page: Page) => page.getByRole('button', { name: '采用到编辑区', exact: true })
const save = (page: Page) => page.getByRole('button', { name: '确认补充分配', exact: true })
const row = (page: Page, name: string) =>
  page.locator('.allocation-target-row').filter({ hasText: name })
// 界面按币种精度显示与接受十进制金额；夹具里的期望值是最小单位，需换算后比较。
function decimalCNY(minor: number): string {
  return `${Math.trunc(minor / 100)}.${String(minor % 100).padStart(2, '0')}`
}

function gate() {
  let release = () => {}
  const promise = new Promise<void>((resolve) => (release = resolve))
  return { promise, release }
}
async function setup(page: Page, scenarios = suggestionScenarios()) {
  const state = await mockSuggestionWorkspace(page, scenarios)
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  checks.set(page, { errors, unexpected: state.unexpected })
  return state
}
async function open(page: Page, scenario = suggestionScenarios()[0]!) {
  await page.goto(path(scenario))
  await expect(page.getByRole('heading', { name: '分配草案（未保存）', exact: true })).toBeVisible()
}
async function prepare(page: Page) {
  await adopt(page).click()
  await page.getByLabel(/调整理由/).fill('合成核对理由')
}
test.afterEach(async ({ page }) => {
  expect(checks.get(page)?.errors).toEqual([])
  expect(checks.get(page)?.unexpected).toEqual([])
})

test('相同 8 个固定场景：实际采用与手工输入，完整计划一致', async ({ page }, info) => {
  const scenarios = suggestionScenarios()
  const state = await setup(page, scenarios)
  const counts = {
    target_selections: 0,
    suggestion_adoptions: 0,
    amount_fills: 0,
    reason_fills: 0,
    explicit_saves: 0,
    queries: 0,
  }
  for (const [index, scenario] of scenarios.entries()) {
    await open(page, scenario)
    expect(state.writes).toHaveLength(index)
    for (const target of scenario.workspace.targets) {
      if (!target.current_link_id)
        await expect(row(page, target.display_name).getByRole('checkbox')).not.toBeChecked()
    }
    if (scenario.kind !== 'manual') {
      await page
        .getByRole('button', {
          name: scenario.kind === 'exact' ? '采用到编辑区' : '采用目标，金额由我填写',
          exact: true,
        })
        .click()
      counts.suggestion_adoptions++
    }
    for (const desired of scenario.chosen) {
      const target = scenario.workspace.targets.find(
        (target) => target.id === desired.target_fact_id,
      )!
      if (target.current_link_id) continue
      const targetRow = row(page, target.display_name)
      if (scenario.kind === 'manual') {
        await targetRow.getByRole('checkbox').check()
        counts.target_selections++
      }
      const amount = targetRow.getByLabel('分配金额', { exact: true })
      if (scenario.kind !== 'exact') {
        await expect(amount).toHaveValue('')
        await amount.fill(decimalCNY(desired.allocated_minor))
        counts.amount_fills++
      } else await expect(amount).toHaveValue(decimalCNY(desired.allocated_minor))
    }
    expect(state.writes).toHaveLength(index)
    await page.getByLabel(/调整理由/).fill('合成核对理由')
    counts.reason_fills++
    await save(page).click()
    counts.explicit_saves++
    await expect(page.getByText('补充分配已保存，余额已刷新', { exact: true })).toBeVisible()
    expect(state.writes[index]!.body).toEqual({
      expected_plan_hash: scenario.workspace.plan_hash,
      desired_allocations: scenario.chosen,
      reason: '合成核对理由',
    })
    expect(state.writes[index]!.anchorId).toBe(scenario.workspace.anchor.id)
  }
  expect(state.writes).toHaveLength(8)
  expect(new Set(state.writes.map((write) => write.key)).size).toBe(8)
  expect(state.writes.every((write) => Boolean(write.key))).toBe(true)
  await info.attach('interaction-counts.json', {
    contentType: 'application/json',
    body: JSON.stringify({
      fixture_version: 'allocation-suggestion-fixtures/1',
      scenarios: 8,
      counts,
      total_actions: Object.values(counts).reduce((sum, count) => sum + count, 0),
      correct_complete_plans: 8,
      distinct_idempotency_keys: 8,
      page_errors: checks.get(page)!.errors.length,
      unexpected_requests: state.unexpected.length,
      actual_model_evaluated: false,
      human_time_measured: false,
    }),
  })
})

test('建议不自动写入；键盘采用后聚焦唯一编辑区，理由必填', async ({ page }, info) => {
  const state = await setup(page)
  await open(page)
  await expect(page.locator('.suggestion-panel')).toContainText('不能证明同一交易')
  await expect(page.locator('.suggestion-panel')).toContainText('本次读取快照')
  await adopt(page).focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('heading', { name: '选择分配单据' })).toBeFocused()
  await expect(page.getByLabel(/调整理由/)).toHaveValue('')
  await save(page).click()
  await expect(page.getByText('请填写本次调整理由', { exact: true })).toBeVisible()
  expect(state.writes).toHaveLength(0)
  await page.getByLabel(/调整理由/).fill('合成显式核对')
  await captureResponsiveReview(page, info, 'allocation-suggestion-adopted')
})

test('部分金额留空并聚焦，不默认填满上限', async ({ page }) => {
  const scenario = suggestionScenarios()[5]!
  const state = await setup(page, [scenario])
  await open(page, scenario)
  await page.getByRole('button', { name: '采用目标，金额由我填写', exact: true }).click()
  const amount = page.getByLabel('分配金额', { exact: true })
  await expect(amount).toHaveValue('')
  await expect(amount).toBeFocused()
  await page.getByLabel(/调整理由/).fill('合成部分分配')
  await save(page).click()
  await expect(page.getByText('请填写金额', { exact: true })).toBeVisible()
  expect(state.writes).toHaveLength(0)
})

test('采用后修改金额，保留预先填写的人工理由', async ({ page }) => {
  const state = await setup(page)
  await open(page)
  await page.getByLabel(/调整理由/).fill('我的合成核对理由')
  await adopt(page).click()
  await expect(page.getByLabel(/调整理由/)).toHaveValue('我的合成核对理由')
  await page.getByLabel('分配金额', { exact: true }).fill('23.00')
  await save(page).click()
  await expect(page.getByText('补充分配已保存，余额已刷新', { exact: true })).toBeVisible()
  expect(state.writes[0]!.body.desired_allocations[0]!.allocated_minor).toBe(2300)
  expect(state.writes[0]!.body.reason).toBe('我的合成核对理由')
})

test('手工目标或金额不被覆盖，可拒绝建议继续手工编辑', async ({ page }) => {
  const state = await setup(page)
  await open(page)
  const target = page.locator('.allocation-target-row')
  await target.getByRole('checkbox').check()
  await target.getByLabel('分配金额', { exact: true }).fill('17.00')
  await expect(adopt(page)).toBeDisabled()
  await target.getByRole('checkbox').uncheck()
  await expect(adopt(page)).toBeDisabled()
  await page.getByRole('button', { name: '不采用草案', exact: true }).click()
  await expect(page.getByText('本次未采用草案，可在下方手工分配。')).toBeVisible()
  await expect(target.getByLabel('分配金额', { exact: true })).toHaveValue('17.00')
  expect(state.writes).toHaveLength(0)
})

test('初始分页不完整，最后一页和搜索不重新冒充完整集合', async ({ page }) => {
  const scenario = suggestionScenarios()[0]!
  scenario.workspace.next_cursor = 'synthetic-next-page'
  const state = await setup(page, [scenario])
  await open(page, scenario)
  await expect(page.locator('.suggestion-panel')).toContainText('尚未完整加载')
  delete state.workspaces.get(scenario.workspace.anchor.id)!.next_cursor
  await page.getByRole('button', { name: '下一页候选', exact: true }).click()
  await expect(page.locator('.suggestion-panel')).toContainText('已进行搜索或翻页')
  await expect(adopt(page)).toHaveCount(0)
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(adopt(page)).toBeEnabled()
  await page.getByLabel('查找分配单据', { exact: true }).fill('合成')
  await page.getByRole('button', { name: '查询单据', exact: true }).click()
  await expect(adopt(page)).toHaveCount(0)
  await page.getByRole('link', { name: '返回支付列表', exact: true }).click()
  await expect(page).toHaveURL('/payments')
})

test('刷新和离开前提示，取消保留草稿，确认刷新重建建议', async ({ page }) => {
  await setup(page)
  await open(page)
  await prepare(page)
  page.once('dialog', (dialog) => dialog.dismiss())
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(page.getByLabel(/调整理由/)).toHaveValue('合成核对理由')
  page.once('dialog', (dialog) => dialog.dismiss())
  await page.getByRole('link', { name: '返回支付列表', exact: true }).click()
  await expect(page).toHaveURL(path(suggestionScenarios()[0]!))
  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await expect(adopt(page)).toBeEnabled()
  await expect(page.getByLabel(/调整理由/)).toHaveValue('')
  await expect(page.locator('.allocation-target-row').getByRole('checkbox')).not.toBeChecked()
})

test('409 保留编辑但旧建议和再次提交失效，刷新后重新核对', async ({ page }) => {
  const scenario = suggestionScenarios()[0]!
  const state = await setup(page, [scenario])
  await open(page, scenario)
  await prepare(page)
  state.workspaces.get(scenario.workspace.anchor.id)!.plan_hash = 'e'.repeat(64)
  await save(page).click()
  await expect(page.getByText(/合成计划已变化。当前草稿已保留/)).toBeVisible()
  await expect(save(page)).toBeDisabled()
  await expect(adopt(page)).toHaveCount(0)
  await expect(page.getByLabel(/调整理由/)).toHaveValue('合成核对理由')
  page.once('dialog', (dialog) => dialog.accept())
  await page.getByRole('button', { name: '刷新当前分配', exact: true }).click()
  await prepare(page)
  await save(page).click()
  await expect(page.getByText('补充分配已保存，余额已刷新', { exact: true })).toBeVisible()
  expect(state.writes).toHaveLength(2)
  expect(state.writes[0]!.key).not.toBe(state.writes[1]!.key)
})

test('未知结果冻结输入，显式重试使用原完整请求与原键', async ({ page }) => {
  const scenario = suggestionScenarios()[0]!
  const state = await setup(page, [scenario])
  const requests: Array<{ body: unknown; key: string | undefined }> = []
  await page.route(`${workspaceAPI(scenario)}/adjustments`, async (route) => {
    requests.push({
      body: route.request().postDataJSON(),
      key: route.request().headers()['idempotency-key'],
    })
    if (requests.length === 1)
      await route.fulfill({
        status: 503,
        json: { error: { code: 'synthetic_unavailable', message: '合成暂时不可用' } },
      })
    else await route.fallback()
  })
  await open(page, scenario)
  await prepare(page)
  await save(page).click()
  await expect(page.getByText(/上次保存结果未知/)).toBeVisible()
  await expect(page.getByLabel(/调整理由/)).toBeDisabled()
  await expect(page.getByLabel('分配金额', { exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '重试原分配', exact: true }).click()
  await expect(page.getByText('补充分配已保存，余额已刷新', { exact: true })).toBeVisible()
  expect(requests).toHaveLength(2)
  expect(requests[0]).toEqual(requests[1])
  expect(state.writes).toHaveLength(1)
})

test('提交期间冻结编辑、搜索、刷新与导航', async ({ page }) => {
  const scenario = suggestionScenarios()[0]!
  const state = await setup(page, [scenario])
  const response = gate()
  let sent = false
  await page.route(`${workspaceAPI(scenario)}/adjustments`, async (route) => {
    sent = true
    await response.promise
    await route.fallback()
  })
  await open(page, scenario)
  await prepare(page)
  await save(page).click()
  await expect.poll(() => sent).toBe(true)
  await expect(page.getByLabel(/调整理由/)).toBeDisabled()
  await expect(page.getByLabel('查找分配单据', { exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: '刷新', exact: true })).toBeDisabled()
  await expect(page.locator('.allocation-target-row').getByRole('checkbox')).toBeDisabled()
  await page.getByRole('link', { name: '返回支付列表', exact: true }).click()
  await expect(page).toHaveURL(path(scenario))
  response.release()
  await expect(page.getByText('补充分配已保存，余额已刷新', { exact: true })).toBeVisible()
  expect(state.writes).toHaveLength(1)
})

test('保存成功但重读失败，明确已保存且重试只读不重提', async ({ page }) => {
  const scenario = suggestionScenarios()[0]!
  const state = await setup(page, [scenario])
  let reads = 0
  await page.route(workspaceAPI(scenario), async (route) => {
    reads++
    if (reads === 2)
      await route.fulfill({
        status: 503,
        json: { error: { code: 'synthetic_read_failure', message: '合成读取失败' } },
      })
    else await route.fallback()
  })
  await open(page, scenario)
  await prepare(page)
  await save(page).click()
  await expect(
    page.getByText('补充分配已保存，但工作区刷新失败。可重试读取，无需再次提交。', { exact: true }),
  ).toBeVisible()
  await page.getByRole('button', { name: '重试', exact: true }).click()
  await expect(page.getByRole('heading', { name: '当前 支付', exact: true })).toBeVisible()
  expect(state.writes).toHaveLength(1)
})

test('离开时迟到的工作区读取不能重建旧页', async ({ page }) => {
  const scenario = suggestionScenarios()[0]!
  await setup(page, [scenario])
  const response = gate()
  const started = gate()
  await page.route(workspaceAPI(scenario), async (route) => {
    started.release()
    await response.promise
    await route.fallback()
  })
  await page.goto(path(scenario))
  await started.promise
  await page.getByRole('link', { name: '返回支付列表', exact: true }).click()
  await expect(page).toHaveURL('/payments')
  const delivered = page.waitForResponse(
    (result) => new URL(result.url()).pathname === `/api/v1${path(scenario)}`,
  )
  response.release()
  await (await delivered).finished()
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())))
  await expect(page.getByRole('heading', { name: '支付管理', exact: true })).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
  await expect(page.locator('.suggestion-panel')).toHaveCount(0)
})

test('退出会话后迟到保存不得恢复旧工作区', async ({ page }) => {
  const scenario = suggestionScenarios()[0]!
  const state = await setup(page, [scenario])
  const response = gate()
  const started = gate()
  await page.route(`${workspaceAPI(scenario)}/adjustments`, async (route) => {
    started.release()
    await response.promise
    await route.fallback()
  })
  await open(page, scenario)
  await prepare(page)
  await save(page).click()
  await started.promise
  await page.getByRole('button', { name: '退出', exact: true }).click()
  await expect(page).toHaveURL(/\/login/)
  const delivered = page.waitForResponse(
    (result) => new URL(result.url()).pathname === `/api/v1${path(scenario)}/adjustments`,
  )
  response.release()
  await (await delivered).finished()
  await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())))
  await expect.poll(() => state.writes.length).toBe(1)
  await expect(page.locator('.suggestion-panel')).toHaveCount(0)
  await expect(page.getByText(/分配已保存/)).toHaveCount(0)
})

test('采用前建议在四档宽度和浅深主题可核对', async ({ page }, info) => {
  const scenario = suggestionScenarios()[4]!
  const state = await setup(page, [scenario])
  await open(page, scenario)
  await expect(page.locator('.suggestion-panel')).toContainText('保留当前 1 条关联及其金额')
  await expect(page.locator('.suggestion-list li')).toHaveCount(2)
  await captureResponsiveReview(
    page,
    info,
    'allocation-suggestion-preview',
    '.suggestion-panel button:enabled:visible',
  )
  expect(state.writes).toHaveLength(0)
})
