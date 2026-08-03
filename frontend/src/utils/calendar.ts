const clampMonthIndex = (month: number) => {
  if (!Number.isFinite(month)) return 0
  return Math.min(11, Math.max(0, Math.trunc(month)))
}

// PrimeVue month-change 使用 1-12，日期插槽使用 0-11。
export const primeMonthEventToIndex = (month: number) =>
  clampMonthIndex(month - 1)

export const primeDateSlotMonthToIndex = (month: number) =>
  clampMonthIndex(month)
