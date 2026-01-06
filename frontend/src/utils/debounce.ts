export type DebouncedFn<TArgs extends any[]> = ((...args: TArgs) => void) & {
  cancel: () => void
}

export const debounce = <TArgs extends any[]>(
  fn: (...args: TArgs) => void,
  waitMs = 200,
): DebouncedFn<TArgs> => {
  let timer: number | null = null

  const debounced = ((...args: TArgs) => {
    if (timer) window.clearTimeout(timer)
    timer = window.setTimeout(() => {
      timer = null
      fn(...args)
    }, waitMs)
  }) as DebouncedFn<TArgs>

  debounced.cancel = () => {
    if (timer) window.clearTimeout(timer)
    timer = null
  }

  return debounced
}

