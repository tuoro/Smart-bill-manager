import { describe, expect, it } from 'vitest'
import type { AllocationWorkspace } from '../../data/client'
import { allocationDraftChanged, createAllocationDraft } from './model'
import { buildAllocationSuggestion } from './suggestion'

type Target = AllocationWorkspace['targets'][number]
type Anchor = AllocationWorkspace['anchor']

describe('allocation suggestion', () => {
  it.each(['payment', 'invoice'] as const)(
    'prepares an equal-balance suggestion from a %s to the opposite type',
    (factType) => {
      const candidate = target('new-target', 1_000, {
        fact_type: factType === 'payment' ? 'invoice' : 'payment',
      })
      const result = buildAllocationSuggestion(workspace([candidate], { fact_type: factType }))

      expect(result.status).toBe('ready')
      expect(result.items).toEqual([{ target: candidate, amountMinor: 1_000, maximumMinor: 1_000 }])
      expect(result.remainingMinor).toBe(0)
    },
  )

  it('uses every eligible target when their remaining balances exactly match', () => {
    const laterID = target('target-b', 650)
    const earlierID = target('target-a', 350)
    const result = buildAllocationSuggestion(workspace([laterID, earlierID]))

    expect(result.status).toBe('ready')
    expect(result.items).toEqual([
      { target: earlierID, amountMinor: 350, maximumMinor: 350 },
      { target: laterID, amountMinor: 650, maximumMinor: 650 },
    ])
    expect(result.remainingMinor).toBe(0)
  })

  it.each([600, 1_400])(
    'leaves the amount blank when the only target has a different balance of %i',
    (remainingMinor) => {
      const candidate = target('partial-target', remainingMinor)
      const result = buildAllocationSuggestion(workspace([candidate]))

      expect(result.status).toBe('amount_required')
      expect(result.items).toEqual([
        {
          target: candidate,
          amountMinor: null,
          maximumMinor: Math.min(1_000, remainingMinor),
        },
      ])
      expect(result.remainingMinor).toBeNull()
    },
  )

  it.each([
    { name: 'balances below the anchor', amounts: [300, 400] },
    { name: 'balances above the anchor', amounts: [600, 500] },
    { name: 'an exact first target and an exact subset', amounts: [1_000, 600, 400] },
  ])('does not choose or combine a subset for $name', ({ amounts }) => {
    const candidates = amounts.map((amount, index) => target(`target-${index}`, amount))
    const result = buildAllocationSuggestion(workspace(candidates))

    expect(result.status).toBe('ambiguous')
    expect(result.items).toEqual([])
    expect(result.remainingMinor).toBe(1_000)
  })

  it.each([
    { name: 'an empty candidate set', targets: [] },
    { name: 'a name mismatch', targets: [target('other-name', 1_000, { name_exact: false })] },
  ])('provides no suggestion for $name', ({ targets }) => {
    const result = buildAllocationSuggestion(workspace(targets))

    expect(result.status).toBe('none')
    expect(result.items).toEqual([])
    expect(result.remainingMinor).toBe(1_000)
  })

  it('leaves existing links and their amounts intact, including links outside 30 days', () => {
    const current = linkedTarget('existing-target', 400, {
      amount_minor: 900,
      remaining_minor: 500,
      maximum_allocatable_minor: 900,
      date_distance_days: 90,
    })
    const candidate = target('new-target', 600)
    const input = workspace([current, candidate])
    const original = structuredClone(input)
    const draft = createAllocationDraft(input)
    const result = buildAllocationSuggestion(input)

    expect(result.status).toBe('ready')
    expect(result.items).toEqual([{ target: candidate, amountMinor: 600, maximumMinor: 600 }])
    expect(draft).toEqual([
      { target: current, selected: true, amountText: '4.00' },
      { target: candidate, selected: false, amountText: '' },
    ])
    expect(input).toEqual(original)
  })

  it('does not increase an existing link even when its target has matching free balance', () => {
    const input = workspace([
      linkedTarget('existing-target', 400, {
        amount_minor: 1_000,
        remaining_minor: 600,
        maximum_allocatable_minor: 1_000,
      }),
    ])

    expect(buildAllocationSuggestion(input)).toMatchObject({
      status: 'none',
      items: [],
      remainingMinor: 600,
    })
  })

  it('does not propose replacing existing links when the anchor is fully allocated', () => {
    const input = workspace([linkedTarget('existing-target', 1_000), target('new-target')])

    expect(buildAllocationSuggestion(input)).toMatchObject({
      status: 'none',
      items: [],
      remainingMinor: 0,
    })
  })

  it('rejects an incomplete initial page', () => {
    const input = { ...workspace(), next_cursor: 'synthetic-next-page' }

    expect(buildAllocationSuggestion(input)).toMatchObject({
      status: 'unavailable',
      items: [],
      remainingMinor: null,
    })
  })

  it('does not treat a searched or last-page candidate set as the complete initial scope', () => {
    expect(buildAllocationSuggestion(workspace(), false)).toMatchObject({
      status: 'unavailable',
      items: [],
      remainingMinor: null,
    })
    expect(buildAllocationSuggestion(workspace(), true).status).toBe('ready')
  })

  it.each([
    { count: 200, status: 'ready' },
    { count: 201, status: 'unavailable' },
  ] as const)('bounds a new plan with $count targets as $status', ({ count, status }) => {
    const candidates = Array.from({ length: count }, (_, index) => target(`target-${index}`, 1))
    const result = buildAllocationSuggestion(workspace(candidates, { amount_minor: count }))

    expect(result.status).toBe(status)
    expect(result.items).toHaveLength(status === 'ready' ? count : 0)
  })

  it.each([
    { currentCount: 199, status: 'ready' },
    { currentCount: 200, status: 'unavailable' },
  ] as const)(
    'counts $currentCount existing links toward the full plan limit',
    ({ currentCount, status }) => {
      const current = Array.from({ length: currentCount }, (_, index) =>
        linkedTarget(`existing-${index}`, 1),
      )
      const candidate = target('new-target', 1)
      const input = workspace([...current, candidate], { amount_minor: currentCount + 1 })
      const result = buildAllocationSuggestion(input)

      expect(result.status).toBe(status)
      expect(result.items).toHaveLength(status === 'ready' ? 1 : 0)
      expect(input.links).toHaveLength(currentCount)
    },
  )

  it('allows safe integer balances and totals at the exact maximum', () => {
    const input = workspace(
      [target('target-a', Number.MAX_SAFE_INTEGER - 1), target('target-b', 1)],
      { amount_minor: Number.MAX_SAFE_INTEGER },
    )
    const result = buildAllocationSuggestion(input)

    expect(result.status).toBe('ready')
    expect(result.items.map((item) => item.amountMinor)).toEqual([Number.MAX_SAFE_INTEGER - 1, 1])
    expect(result.remainingMinor).toBe(0)
  })

  it.each<Partial<Anchor>>([
    { amount_minor: Number.MAX_SAFE_INTEGER + 1 },
    { allocated_minor: -1 },
    { remaining_minor: 1.5 },
    { remaining_minor: Number.NaN },
    { remaining_minor: Number.POSITIVE_INFINITY },
    { remaining_minor: 999 },
  ])('rejects unsafe or inconsistent anchor balances %j', (overrides) => {
    expect(buildAllocationSuggestion(workspace(undefined, overrides))).toMatchObject({
      status: 'unavailable',
      items: [],
      remainingMinor: null,
    })
  })

  it.each<Partial<Target>>([
    { amount_minor: Number.MAX_SAFE_INTEGER + 1 },
    { allocated_minor: -1 },
    { remaining_minor: 1.5 },
    { maximum_allocatable_minor: Number.POSITIVE_INFINITY },
    { maximum_allocatable_minor: Number.NaN },
    { remaining_minor: 999 },
    { maximum_allocatable_minor: 999 },
  ])('rejects unsafe or inconsistent candidate balances %j', (overrides) => {
    const input = workspace([target('new-target', 1_000, overrides)])

    expect(buildAllocationSuggestion(input)).toMatchObject({
      status: 'unavailable',
      items: [],
      remainingMinor: null,
    })
  })

  it('rejects an unsafe sum even when each candidate balance is a safe integer', () => {
    const input = workspace([target('target-a', Number.MAX_SAFE_INTEGER), target('target-b', 1)], {
      amount_minor: Number.MAX_SAFE_INTEGER,
    })

    expect(buildAllocationSuggestion(input)).toMatchObject({
      status: 'unavailable',
      items: [],
      remainingMinor: null,
    })
  })

  it.each([0, 30])('includes the allowed date-distance boundary of %i days', (days) => {
    const input = workspace([target('new-target', 1_000, { date_distance_days: days })])

    expect(buildAllocationSuggestion(input).status).toBe('ready')
  })

  it.each<Partial<Target>>([
    { currency: 'USD' },
    { fact_type: 'payment' },
    { date_distance_days: -1 },
    { date_distance_days: 31 },
    { date_distance_days: 0.5 },
    { date_distance_days: Number.NaN },
    { amount_minor: 0, remaining_minor: 0, maximum_allocatable_minor: 0 },
  ])('excludes a target outside the recommendation boundary %j', (overrides) => {
    const input = workspace([target('ineligible-target', 1_000, overrides)])

    expect(buildAllocationSuggestion(input)).toMatchObject({
      status: 'none',
      items: [],
      remainingMinor: 1_000,
    })
  })

  it('does not let ineligible targets change an otherwise exact suggestion', () => {
    const eligible = target('eligible-target')
    const input = workspace([
      target('other-currency', 500, { currency: 'USD' }),
      target('outside-window', 500, { date_distance_days: 31 }),
      target('other-name', 500, { name_exact: false }),
      eligible,
    ])

    expect(buildAllocationSuggestion(input)).toMatchObject({
      status: 'ready',
      items: [{ target: eligible, amountMinor: 1_000, maximumMinor: 1_000 }],
    })
  })

  it('refuses a workspace whose current link target is missing', () => {
    const input = workspace([linkedTarget('existing-target', 400), target('new-target', 600)])
    input.targets = input.targets.filter((entry) => !entry.current_link_id)

    expect(buildAllocationSuggestion(input).status).toBe('unavailable')
  })

  it.each<Partial<Target>>([
    { current_link_id: 'different-link' },
    { current_allocated_minor: 399 },
  ])('refuses a workspace whose current link disagrees with its target %j', (overrides) => {
    const input = workspace([linkedTarget('existing-target', 400), target('new-target', 600)])
    input.targets = input.targets.map((entry) =>
      entry.current_link_id ? { ...entry, ...overrides } : entry,
    )

    expect(buildAllocationSuggestion(input).status).toBe('unavailable')
  })

  it('does not count a duplicated target twice', () => {
    const input = workspace([target('same-target', 500), target('same-target', 500)])

    expect(buildAllocationSuggestion(input).status).toBe('unavailable')
  })

  it('does not mutate the workspace while calculating and ordering suggestions', () => {
    const input = workspace([target('target-z', 300), target('target-a', 700)])
    const original = structuredClone(input)

    expect(buildAllocationSuggestion(input).items.map((item) => item.target.id)).toEqual([
      'target-a',
      'target-z',
    ])
    expect(input).toEqual(original)
    expect(
      createAllocationDraft(input).every((row) => !row.selected && row.amountText === ''),
    ).toBe(true)
  })
})

describe('allocation draft change protection', () => {
  it('allows an untouched draft regardless of row order', () => {
    const input = workspace([linkedTarget('existing-target', 400), target('new-target', 600)])
    const rows = createAllocationDraft(input)

    expect(allocationDraftChanged(input, rows)).toBe(false)
    expect(allocationDraftChanged(input, [...rows].reverse())).toBe(false)
  })

  it('ignores candidate-only search and pagination changes when no input was made', () => {
    const input = workspace([linkedTarget('existing-target', 400), target('initial-target', 600)])
    const currentRows = createAllocationDraft(input).filter((row) => row.selected)
    const searchRow = { target: target('searched-target'), selected: false, amountText: '' }

    expect(allocationDraftChanged(input, currentRows)).toBe(false)
    expect(allocationDraftChanged(input, [...currentRows, searchRow])).toBe(false)
    expect(allocationDraftChanged(input, [...createAllocationDraft(input), searchRow])).toBe(false)
  })

  it('protects a manually selected new target before its amount is entered', () => {
    const input = workspace()
    const rows = createAllocationDraft(input).map((row) => ({ ...row, selected: true }))

    expect(allocationDraftChanged(input, rows)).toBe(true)
  })

  it.each(['250', '0'])(
    'protects entered amount %s even when the target remains unselected',
    (amountText) => {
      const input = workspace()
      const rows = createAllocationDraft(input).map((row) => ({ ...row, amountText }))

      expect(allocationDraftChanged(input, rows)).toBe(true)
    },
  )

  it('protects an amount entered on an unselected search result', () => {
    const input = workspace()
    const rows = [
      ...createAllocationDraft(input),
      { target: target('searched-target'), selected: false, amountText: '1.25' },
    ]

    expect(allocationDraftChanged(input, rows)).toBe(true)
  })

  it.each([
    { selected: false, amountText: '' },
    { selected: true, amountText: '3.99' },
    { selected: true, amountText: '' },
  ])('protects edits to an existing allocation %j', (edit) => {
    const input = workspace([linkedTarget('existing-target', 400)])
    const rows = createAllocationDraft(input).map((row) => ({ ...row, ...edit }))

    expect(allocationDraftChanged(input, rows)).toBe(true)
  })

  it('recognizes a reverted draft and does not mutate either input', () => {
    const input = workspace([linkedTarget('existing-target', 400), target('new-target', 600)])
    const rows = [...createAllocationDraft(input)].reverse()
    rows[0]!.selected = true
    rows[0]!.amountText = '600'
    expect(allocationDraftChanged(input, rows)).toBe(true)

    rows[0]!.selected = false
    rows[0]!.amountText = ''
    const originalWorkspace = structuredClone(input)
    const originalRows = structuredClone(rows)

    expect(allocationDraftChanged(input, rows)).toBe(false)
    expect(input).toEqual(originalWorkspace)
    expect(rows).toEqual(originalRows)
  })
})

function target(id: string, remainingMinor = 1_000, overrides: Partial<Target> = {}): Target {
  return {
    fact_type: 'invoice',
    id,
    amount_minor: remainingMinor,
    allocated_minor: 0,
    remaining_minor: remainingMinor,
    currency: 'CNY',
    business_date: '2026-09-06',
    display_name: '合成商户',
    name_exact: true,
    date_distance_days: 0,
    current_allocated_minor: 0,
    maximum_allocatable_minor: remainingMinor,
    ...overrides,
  }
}

function linkedTarget(id: string, allocatedMinor: number, overrides: Partial<Target> = {}): Target {
  return target(id, 0, {
    amount_minor: allocatedMinor,
    allocated_minor: allocatedMinor,
    current_link_id: `link-${id}`,
    current_allocated_minor: allocatedMinor,
    maximum_allocatable_minor: allocatedMinor,
    ...overrides,
  })
}

function workspace(
  targets: Target[] = [target('new-target')],
  anchorOverrides: Partial<Anchor> = {},
): AllocationWorkspace {
  const links = targets.flatMap((entry) =>
    entry.current_link_id
      ? [
          {
            id: entry.current_link_id,
            target_fact_type: entry.fact_type,
            target_fact_id: entry.id,
            allocated_minor: entry.current_allocated_minor,
            currency: entry.currency,
            created_at: '2026-09-06T08:00:00Z',
          },
        ]
      : [],
  )
  const amountMinor = anchorOverrides.amount_minor ?? 1_000
  const allocatedMinor = links.reduce((sum, link) => sum + link.allocated_minor, 0)
  return {
    anchor: {
      fact_type: 'payment',
      id: 'anchor-fact',
      amount_minor: amountMinor,
      allocated_minor: allocatedMinor,
      remaining_minor: amountMinor - allocatedMinor,
      currency: 'CNY',
      business_date: '2026-09-06',
      display_name: '合成商户',
      ...anchorOverrides,
    },
    links,
    targets,
    plan_hash: 'a'.repeat(64),
  }
}
