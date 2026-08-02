import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { User } from '@/types'

const authApiMock = vi.hoisted(() => ({
  login: vi.fn(),
  inviteRegister: vi.fn(),
  setup: vi.fn(),
  verify: vi.fn(),
  checkSetupRequired: vi.fn(),
}))

const storageMock = vi.hoisted(() => ({
  clearAuth: vi.fn(),
  getStoredUser: vi.fn(),
  getToken: vi.fn(),
  setStoredUser: vi.fn(),
  setToken: vi.fn(),
}))

vi.mock('@/api/auth', () => ({ authApi: authApiMock }))
vi.mock('@/api/storage', () => storageMock)

import { useAuthStore } from './auth'

const user: User = {
  id: 'user-1',
  username: 'tester',
  role: 'user',
  is_active: 1,
}

describe('useAuthStore', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    storageMock.getStoredUser.mockReturnValue(null)
    storageMock.getToken.mockReturnValue(null)
    setActivePinia(createPinia())
  })

  it('只有用户和令牌同时存在时才视为已登录', () => {
    storageMock.getStoredUser.mockReturnValue(user)

    const store = useAuthStore()

    expect(store.user).toEqual(user)
    expect(store.isAuthenticated).toBe(false)
  })

  it('邀请码注册成功后同步持久化和内存会话', async () => {
    authApiMock.inviteRegister.mockResolvedValue({
      data: { success: true, message: 'ok', user, token: 'token-1' },
    })
    const store = useAuthStore()

    await expect(store.registerWithInvite('invite-1', 'tester', 'secret')).resolves.toEqual({
      success: true,
      message: '注册成功',
    })
    expect(storageMock.setToken).toHaveBeenCalledWith('token-1')
    expect(storageMock.setStoredUser).toHaveBeenCalledWith(user)
    expect(store.user).toEqual(user)
    expect(store.isAuthenticated).toBe(true)
  })

  it('成功响应缺少会话数据时拒绝建立半成品会话', async () => {
    authApiMock.setup.mockResolvedValue({ data: { success: true, user } })
    const store = useAuthStore()

    const result = await store.setupAdmin('admin', 'secret')

    expect(result.success).toBe(false)
    expect(result.message).toContain('会话数据不完整')
    expect(storageMock.setToken).not.toHaveBeenCalled()
    expect(store.isAuthenticated).toBe(false)
  })

  it('认证请求失败时优先返回服务端业务消息', async () => {
    authApiMock.login.mockRejectedValue({ response: { data: { message: '用户名或密码错误' } } })
    const store = useAuthStore()

    await expect(store.login('tester', 'wrong')).resolves.toEqual({
      success: false,
      message: '用户名或密码错误',
    })
  })

  it('本地会话不完整时校验会清空残留状态', async () => {
    storageMock.getStoredUser.mockReturnValue(user)
    const store = useAuthStore()

    await expect(store.verifyToken()).resolves.toBe(false)
    expect(storageMock.clearAuth).toHaveBeenCalledOnce()
    expect(store.user).toBeNull()
  })
})
