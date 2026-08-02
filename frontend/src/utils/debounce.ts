export type DebouncedFn<TArgs extends unknown[]> = ((...args: TArgs) => void) & {
  cancel: () => void
}

export const debounce = <TArgs extends unknown[]>(
  fn: (...args: TArgs) => void,
  waitMs = 200,
): DebouncedFn<TArgs> => {
  let timer: number | null = null

  const debounced = ((...args: TArgs) => {
    if (timer !== null) window.clearTimeout(timer)
    timer = window.setTimeout(() => {
      timer = null
      fn(...args)
    }, waitMs)
  }) as DebouncedFn<TArgs>

  debounced.cancel = () => {
    if (timer !== null) window.clearTimeout(timer)
    timer = null
  }

  return debounced
}
