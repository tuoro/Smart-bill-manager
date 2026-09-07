import type { Page } from '@playwright/test'
import type { AllocationAdjustmentRequest, AllocationWorkspace, Session } from '../src/data/client'

const id = (index: number) => `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`
const timestamp = '2026-09-06T00:00:00Z'
export const suggestionSession: Session = {
  user: { id: id(1), email: 'suggestion@example.test', display_name: '合成整理人' },
  tenant: { id: id(2), name: '合成分配工作区', default_currency: 'CNY', timezone: 'Asia/Shanghai' },
  role: 'owner',
  capabilities: ['facts.read', 'allocations.manage'],
  csrf_token: 'synthetic-suggestion-csrf',
  expires_at: '2030-01-01T00:00:00Z',
}

export type SuggestionScenario = {
  name: string
  workspace: AllocationWorkspace
  chosen: Array<{ target_fact_id: string; allocated_minor: number }>
  kind: 'exact' | 'amount_required' | 'manual'
}

export function suggestionScenarios(): SuggestionScenario[] {
  return [
    { name: 'payment_single', type: 'payment', amounts: [10000], chosen: [10000] },
    { name: 'invoice_single', type: 'invoice', amounts: [6000], chosen: [6000], total: 6000 },
    { name: 'payment_multiple', type: 'payment', amounts: [4000, 6000], chosen: [4000, 6000] },
    {
      name: 'invoice_multiple',
      type: 'invoice',
      amounts: [2000, 3000, 5000],
      chosen: [2000, 3000, 5000],
    },
    {
      name: 'preserve_existing',
      type: 'payment',
      amounts: [4000, 3000, 7000],
      chosen: [4000, 3000, 7000],
      existing: true,
      total: 14000,
    },
    {
      name: 'partial_amount',
      type: 'payment',
      amounts: [16000],
      chosen: [4000],
      kind: 'amount_required',
    },
    {
      name: 'competing_targets',
      type: 'payment',
      amounts: [10000, 6000],
      chosen: [5000, 0],
      kind: 'manual',
    },
    { name: 'name_mismatch', type: 'invoice', amounts: [10000], chosen: [10000], kind: 'manual' },
  ].map((scenario, index) => {
    const total = scenario.total ?? 10000
    const existing = scenario.existing ? 4000 : 0
    const targets: AllocationWorkspace['targets'] = scenario.amounts.map((amount, targetIndex) => {
      const linked = Boolean(scenario.existing && targetIndex === 0)
      return {
        fact_type: scenario.type === 'payment' ? 'invoice' : 'payment',
        id: id(1000 + index * 100 + targetIndex),
        amount_minor: amount,
        allocated_minor: linked ? amount : 0,
        remaining_minor: linked ? 0 : amount,
        currency: 'CNY',
        business_date: '2026-09-06',
        display_name: `合成目标 ${index + 1}-${targetIndex + 1}`,
        name_exact: scenario.name !== 'name_mismatch',
        date_distance_days: linked ? 100 : 1,
        current_allocated_minor: linked ? amount : 0,
        maximum_allocatable_minor: amount,
        ...(linked ? { current_link_id: id(9000 + index) } : {}),
      }
    })
    const workspace: AllocationWorkspace = {
      anchor: {
        fact_type: scenario.type as 'payment' | 'invoice',
        id: id(100 + index),
        amount_minor: total,
        allocated_minor: existing,
        remaining_minor: total - existing,
        currency: 'CNY',
        business_date: '2026-09-05',
        display_name: `合成锚点 ${index + 1}`,
      },
      targets,
      links: targets
        .filter((target) => target.current_link_id)
        .map((target) => ({
          id: target.current_link_id!,
          target_fact_type: target.fact_type,
          target_fact_id: target.id,
          allocated_minor: target.current_allocated_minor,
          currency: target.currency,
          created_at: timestamp,
        })),
      plan_hash: String(index + 1).repeat(64),
    }
    return {
      name: scenario.name,
      workspace,
      kind: (scenario.kind ?? 'exact') as SuggestionScenario['kind'],
      chosen: targets.flatMap((target, targetIndex) =>
        scenario.chosen[targetIndex]
          ? [{ target_fact_id: target.id, allocated_minor: scenario.chosen[targetIndex]! }]
          : [],
      ),
    }
  })
}

export async function mockSuggestionWorkspace(page: Page, scenarios = suggestionScenarios()) {
  const state = {
    session: structuredClone(suggestionSession) as Session | null,
    workspaces: new Map(
      scenarios.map((scenario) => [
        scenario.workspace.anchor.id,
        structuredClone(scenario.workspace),
      ]),
    ),
    writes: [] as Array<{
      anchorId: string
      body: AllocationAdjustmentRequest
      key: string | undefined
    }>,
    unexpected: [] as string[],
  }
  const replay = new Map<string, object>()
  await page.route('**/api/v1/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === '/api/v1/session') {
      if (request.method() === 'DELETE') {
        state.session = null
        await route.fulfill({ status: 204 })
      } else if (state.session)
        await route.fulfill({
          json: state.session,
          headers: { 'Set-Cookie': 'sbm_csrf=synthetic-suggestion-csrf; Path=/; SameSite=Strict' },
        })
      else await route.fulfill({ status: 401, json: { error: { code: 'unauthenticated' } } })
      return
    }
    if (['/api/v1/payments', '/api/v1/invoices'].includes(url.pathname)) {
      await route.fulfill({ json: { items: [], next_cursor: '' } })
      return
    }
    const match =
      /^\/api\/v1\/allocations\/(payment|invoice)\/([^/]+)(?:\/(adjustments|targets))?$/.exec(
        url.pathname,
      )
    const workspace = match && state.workspaces.get(match[2]!)
    if (match && workspace) {
      if (!match[3] && request.method() === 'GET') {
        await route.fulfill({ json: workspace })
        return
      }
      if (match[3] === 'targets' && request.method() === 'GET') {
        await route.fulfill({
          json: { items: workspace.targets, next_cursor: workspace.next_cursor },
        })
        return
      }
      if (match[3] === 'adjustments' && request.method() === 'POST') {
        const body = request.postDataJSON() as AllocationAdjustmentRequest
        const key = request.headers()['idempotency-key']
        state.writes.push({ anchorId: workspace.anchor.id, body, key })
        if (key && replay.has(key)) {
          await route.fulfill({ json: { ...replay.get(key), replayed: true } })
          return
        }
        if (!key || body.expected_plan_hash !== workspace.plan_hash) {
          await route.fulfill({
            status: 409,
            json: { error: { code: 'allocation_plan_stale', message: '合成计划已变化' } },
          })
          return
        }
        const old = new Map(workspace.links.map((link) => [link.target_fact_id, link]))
        workspace.links = body.desired_allocations.map((desired, index) => ({
          id: old.get(desired.target_fact_id)?.id ?? id(20000 + index),
          target_fact_type: workspace.anchor.fact_type === 'payment' ? 'invoice' : 'payment',
          target_fact_id: desired.target_fact_id,
          allocated_minor: desired.allocated_minor,
          currency: workspace.anchor.currency,
          created_at: timestamp,
        }))
        workspace.anchor.allocated_minor = workspace.links.reduce(
          (sum, link) => sum + link.allocated_minor,
          0,
        )
        workspace.anchor.remaining_minor =
          workspace.anchor.amount_minor - workspace.anchor.allocated_minor
        workspace.plan_hash = 'f'.repeat(64)
        for (const target of workspace.targets) {
          const others = target.allocated_minor - target.current_allocated_minor
          const link = workspace.links.find((entry) => entry.target_fact_id === target.id)
          target.current_allocated_minor = link?.allocated_minor ?? 0
          target.allocated_minor = others + target.current_allocated_minor
          target.remaining_minor = target.amount_minor - target.allocated_minor
          target.current_link_id = link?.id
        }
        const result = {
          adjustment_id: id(30000 + state.writes.length),
          mode: 'supplement',
          ended_link_ids: [],
          created_link_ids: workspace.links
            .filter((link) => !old.has(link.target_fact_id))
            .map((link) => link.id),
          plan_hash: workspace.plan_hash,
          replayed: false,
        }
        replay.set(key, result)
        await route.fulfill({ json: result })
        return
      }
    }
    state.unexpected.push(`${request.method()} ${url.pathname}`)
    await route.fulfill({ status: 501, json: { error: { code: 'undeclared_synthetic_request' } } })
  })
  return state
}
