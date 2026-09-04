import { expect, type Page, type TestInfo } from '@playwright/test'
import { resolve, sep } from 'node:path'

// 复用已经完成交互断言的纯合成页面，不创建第二套业务 fixture。
export async function captureResponsiveReview(page: Page, testInfo: TestInfo, name: string) {
  const viewport = page.viewportSize()
  if (!viewport) throw new Error('Visual review requires an explicit viewport')
  const focused = await page.evaluateHandle(() => document.activeElement)
  const original = await page.evaluate(() => ({
    theme: document.documentElement.getAttribute('data-theme'),
    savedTheme: localStorage.getItem('sbm_theme'),
    dark: matchMedia('(prefers-color-scheme: dark)').matches,
    reducedMotion: matchMedia('(prefers-reduced-motion: reduce)').matches,
    x: scrollX,
    y: scrollY,
  }))
  try {
    for (const theme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme: theme, reducedMotion: 'reduce' })
      await setTheme(page, theme)
      for (const width of [384, 768, 1024, 1440]) {
        await page.setViewportSize({ width, height: 960 })
        await page.evaluate(async () => {
          scrollTo(0, 0)
          await document.fonts.ready
          await new Promise<void>((done) => requestAnimationFrame(() => done()))
        })
        await expect(page.locator('html')).toHaveAttribute('data-theme', theme)
        expect(
          await page.evaluate(
            () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
          ),
          `${name} ${theme} ${width}px 不应产生页面根横向溢出`,
        ).toBe(true)
        await assertActionButtonContrast(page)
        await page.evaluate(async () => {
          if (document.activeElement instanceof HTMLElement) document.activeElement.blur()
          scrollTo(0, 0)
          await new Promise<void>((done) => requestAnimationFrame(() => done()))
        })
        const path = testInfo.outputPath(`${name}-${theme}-${width}.png`)
        if (!resolve(path).startsWith(`${resolve(testInfo.project.outputDir)}${sep}`)) {
          throw new Error('Visual review screenshots must remain in Playwright outputDir')
        }
        await page.screenshot({ path, fullPage: true, animations: 'disabled' })
      }
    }
  } finally {
    await page.setViewportSize(viewport)
    await page.emulateMedia({
      colorScheme: original.dark ? 'dark' : 'light',
      reducedMotion: original.reducedMotion ? 'reduce' : 'no-preference',
    })
    if (original.theme === 'light' || original.theme === 'dark') {
      await setTheme(page, original.theme)
    }
    await page.evaluate((state) => {
      if (state.theme === null) document.documentElement.removeAttribute('data-theme')
      if (state.savedTheme === null) localStorage.removeItem('sbm_theme')
      else localStorage.setItem('sbm_theme', state.savedTheme)
      scrollTo(state.x, state.y)
    }, original)
    await focused.evaluate((element) => {
      if (element instanceof HTMLElement && element.isConnected) {
        element.focus({ preventScroll: true })
      }
    })
    await focused.dispose()
  }
}

export async function assertActionButtonContrast(page: Page, requirePrimary = false) {
  if (requirePrimary) {
    expect(await page.locator('button.button-primary:enabled:visible').count()).toBeGreaterThan(0)
  }
  const buttons = page.locator(
    'button.button-primary:enabled:visible, button.button-danger:enabled:visible, button.page-number-button[aria-current="page"]:visible',
  )
  for (const button of await buttons.all()) {
    const label = (await button.innerText()).trim()
    expect(label).not.toBe('')
    for (const state of ['normal', 'hover'] as const) {
      if (state === 'hover') await button.hover()
      else await page.mouse.move(0, 0)
      const ratio = await button.evaluate(async (element) => {
        await new Promise<void>((done) => requestAnimationFrame(() => done()))
        await Promise.all(element.getAnimations().map((animation) => animation.finished))
        type Color = [number, number, number, number]
        function color(value: string): Color {
          const match = /^rgba?\((.+)\)$/.exec(value)
          if (!match) throw new Error(`Unsupported computed color: ${value}`)
          const channels = match[1]!.split(/[\s,/]+/).map(Number)
          if (channels.length < 3 || channels.some((channel) => !Number.isFinite(channel))) {
            throw new Error(`Invalid computed color: ${value}`)
          }
          return [channels[0]!, channels[1]!, channels[2]!, channels[3] ?? 1]
        }
        function composite(front: Color, back: Color): Color {
          const alpha = front[3]
          return [
            front[0] * alpha + back[0] * (1 - alpha),
            front[1] * alpha + back[1] * (1 - alpha),
            front[2] * alpha + back[2] * (1 - alpha),
            1,
          ]
        }
        function background(node: Element | null): Color {
          if (!node) return [255, 255, 255, 1]
          const style = getComputedStyle(node)
          if (style.backgroundImage !== 'none' || Number(style.opacity) !== 1) {
            throw new Error('Contrast measurement requires solid backgrounds and full opacity')
          }
          const own = color(style.backgroundColor)
          return own[3] === 1 ? own : composite(own, background(node.parentElement))
        }
        function luminance(value: Color) {
          const channels = value.slice(0, 3).map((channel) => {
            const normalized = channel / 255
            return normalized <= 0.04045
              ? normalized / 12.92
              : ((normalized + 0.055) / 1.055) ** 2.4
          })
          return channels[0]! * 0.2126 + channels[1]! * 0.7152 + channels[2]! * 0.0722
        }
        const surface = background(element)
        const foreground = composite(color(getComputedStyle(element).color), surface)
        const lightness = [luminance(surface), luminance(foreground)]
        return (Math.max(...lightness) + 0.05) / (Math.min(...lightness) + 0.05)
      })
      expect(ratio, `${label} ${state} 文字对比度`).toBeGreaterThanOrEqual(4.5)
    }
  }
  await page.mouse.move(0, 0)
}

async function setTheme(page: Page, theme: 'light' | 'dark') {
  if ((await page.locator('html').getAttribute('data-theme')) === theme) return
  const toggle = page.getByRole('button', {
    name: theme === 'dark' ? '切换到深色模式' : '切换到浅色模式',
    exact: true,
  })
  if ((await toggle.count()) > 0) await toggle.click()
  else
    await page.evaluate((value) => {
      document.documentElement.dataset.theme = value
    }, theme)
}
