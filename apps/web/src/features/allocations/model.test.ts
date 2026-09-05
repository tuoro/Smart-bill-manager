import { describe, expect, it } from 'vitest'
import type { AllocationWorkspace } from '../../data/client'
import { allocationModeLabel, createAllocationDraft, validateAllocationDraft } from './model'

describe('allocation adjustment model', () => {
  it('builds a complete desired plan and derives supplement', () => {
    const workspace = allocationWorkspace()
    const rows = createAllocationDraft(workspace)
    rows[1].selected = true
    rows[1].amountText = '300'
    const result = validateAllocationDraft(workspace, rows, '  补充第二张发票  ', false)

    expect(result.request).toEqual({
      expected_plan_hash: 'a'.repeat(64),
      desired_allocations: [
        { target_fact_id: invoiceA, allocated_minor: 400 },
        { target_fact_id: invoiceB, allocated_minor: 300 },
      ],
      reason: '补充第二张发票',
    })
    expect(allocationModeLabel(workspace, rows)).toBe('补充分配')
  })

  it('requires explicit confirmation before withdrawing all', () => {
    const workspace = allocationWorkspace()
    const rows = createAllocationDraft(workspace)
    rows[0].selected = false

    const blocked = validateAllocationDraft(workspace, rows, '撤销', false)
    expect(blocked.request).toBeUndefined()
    expect(blocked.withdrawAllError).toContain('再次确认')

    const accepted = validateAllocationDraft(workspace, rows, '撤销', true)
    expect(accepted.request?.desired_allocations).toEqual([])
    expect(allocationModeLabel(workspace, rows)).toBe('撤销分配')
  })

  it('derives replace and rejects unchanged, invalid and over-limit plans', () => {
    const workspace = allocationWorkspace()
    const rows = createAllocationDraft(workspace)
    expect(validateAllocationDraft(workspace, rows, '没有变化', false).planError).toBe(
      '分配计划没有变化',
    )

    rows[0].amountText = '601'
    expect(allocationModeLabel(workspace, rows)).toBe('替换分配')
    expect(
      validateAllocationDraft(workspace, rows, '替换', false).targetErrors[invoiceA],
    ).toContain('上限')
    rows[0].amountText = 'abc'
    expect(
      validateAllocationDraft(workspace, rows, '替换', false).targetErrors[invoiceA],
    ).toContain('正整数')
    rows[0].amountText = '400'
    rows[1].selected = true
    rows[1].amountText = '700'
    expect(validateAllocationDraft(workspace, rows, '超出 anchor', false).planError).toContain(
      '账单总额',
    )
  })

  it('requires a bounded reason', () => {
    const workspace = allocationWorkspace()
    const rows = createAllocationDraft(workspace)
    rows[1].selected = true
    rows[1].amountText = '1'
    expect(validateAllocationDraft(workspace, rows, '  ', false).reasonError).toContain('请填写')
    expect(validateAllocationDraft(workspace, rows, '理'.repeat(501), false).reasonError).toContain(
      '500',
    )
  })

  it('keeps the complete selected plan bounded even across candidate pages', () => {
    const workspace = allocationWorkspace()
    const base = createAllocationDraft(workspace)[1]!
    const rows = Array.from({ length: 201 }, (_, index) => ({
      ...base,
      target: { ...base.target, id: `synthetic-target-${index}` },
      selected: true,
      amountText: '1',
    }))
    expect(
      validateAllocationDraft(workspace, rows.slice(0, 200), '合成完整计划', false).request,
    ).toBeDefined()
    expect(validateAllocationDraft(workspace, rows, '合成超限计划', false).planError).toContain(
      '200',
    )
  })
})

const paymentID = '10000000-0000-4000-8000-000000000001'
const invoiceA = '20000000-0000-4000-8000-000000000001'
const invoiceB = '20000000-0000-4000-8000-000000000002'

function allocationWorkspace(): AllocationWorkspace {
  return {
    anchor: {
      fact_type: 'payment',
      id: paymentID,
      amount_minor: 1_000,
      allocated_minor: 400,
      remaining_minor: 600,
      currency: 'CNY',
      business_date: '2026-08-27',
      display_name: '合成商户',
    },
    links: [
      {
        id: '30000000-0000-4000-8000-000000000001',
        target_fact_type: 'invoice',
        target_fact_id: invoiceA,
        allocated_minor: 400,
        currency: 'CNY',
        created_at: '2026-08-27T08:00:00Z',
      },
    ],
    targets: [
      {
        fact_type: 'invoice',
        id: invoiceA,
        amount_minor: 600,
        allocated_minor: 400,
        remaining_minor: 200,
        currency: 'CNY',
        business_date: '2026-08-27',
        display_name: '合成商户',
        name_exact: true,
        date_distance_days: 0,
        current_link_id: '30000000-0000-4000-8000-000000000001',
        current_allocated_minor: 400,
        maximum_allocatable_minor: 600,
      },
      {
        fact_type: 'invoice',
        id: invoiceB,
        amount_minor: 700,
        allocated_minor: 0,
        remaining_minor: 700,
        currency: 'CNY',
        business_date: '2026-08-28',
        display_name: '另一商户',
        name_exact: false,
        date_distance_days: 1,
        current_allocated_minor: 0,
        maximum_allocatable_minor: 700,
      },
    ],
    plan_hash: 'a'.repeat(64),
  }
}
