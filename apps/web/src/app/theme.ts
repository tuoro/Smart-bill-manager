import { ref } from 'vue'

// 登录页与工作台共享一个主题状态，避免路由切换时重置外观。
export const theme = ref<'light' | 'dark'>('light')

export function initializeTheme() {
  const stored = localStorage.getItem('sbm_theme')
  const preferredDark = window.matchMedia('(prefers-color-scheme: dark)').matches
  theme.value = stored === 'dark' || (!stored && preferredDark) ? 'dark' : 'light'
  document.documentElement.dataset.theme = theme.value
}

export function toggleTheme() {
  theme.value = theme.value === 'light' ? 'dark' : 'light'
  document.documentElement.dataset.theme = theme.value
  localStorage.setItem('sbm_theme', theme.value)
}
