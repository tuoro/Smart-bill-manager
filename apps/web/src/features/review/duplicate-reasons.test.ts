import { describe, expect, it } from 'vitest'
import { duplicateReasonLabel } from './model'

// 后端 domain/duplicate.go 里能产生的全部原因码。新增一个而这里没映射，
// 界面就会把内部标识摆给用户看——这条测试就是为了让那种情况当场失败。
const backendReasonCodes = [
  'visual_page_match',
  'within_document',
  'other_document',
  'same_page_count',
  'ordered_page_visual_match',
  'amount_exact',
  'total_exact',
  'currency_exact',
  'merchant_exact',
  'transaction_time_within_5_minutes',
  'order_number_exact',
  'invoice_date_exact',
  'seller_exact',
  'buyer_exact',
]

describe('重复候选的判断依据', () => {
  it('后端能产生的每个原因码都有中文说法', () => {
    for (const code of backendReasonCodes) {
      const label = duplicateReasonLabel(code)
      expect(label, code).not.toBe(code)
      expect(/^[\x20-\x7e]*$/.test(label), `${code} 仍是英文标识`).toBe(false)
    }
  })

  it('漏译时原样返回，而不是留白', () => {
    expect(duplicateReasonLabel('some_future_code')).toBe('some_future_code')
  })
})
