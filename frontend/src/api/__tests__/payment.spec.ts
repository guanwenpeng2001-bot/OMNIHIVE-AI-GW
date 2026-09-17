import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { paymentAPI } from '@/api/payment'

describe('payment api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('keeps legacy public out_trade_no verification for upgrade compatibility', async () => {
    await paymentAPI.verifyOrderPublic('legacy-order-no')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/verify', {
      out_trade_no: 'legacy-order-no',
    })
  })

  it('keeps signed public resume-token resolve endpoint', async () => {
    await paymentAPI.resolveOrderPublicByResumeToken('resume-token-123')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/resolve', {
      resume_token: 'resume-token-123',
    })
  })

  it('forwards the caller cancellation signal for order status requests', async () => {
    const controller = new AbortController()
    await paymentAPI.getOrder(42, controller.signal)
    expect(get).toHaveBeenCalledWith('/payment/orders/42', { signal: controller.signal })
  })
  it('forwards cancellation for authenticated and public verification', async () => {
    const { signal } = new AbortController()
    await paymentAPI.verifyOrder('order', signal)
    await paymentAPI.verifyOrderPublic('order', signal)
    await paymentAPI.resolveOrderPublicByResumeToken('token', signal)
    expect(post).toHaveBeenNthCalledWith(1, '/payment/orders/verify', { out_trade_no: 'order' }, { signal })
    expect(post).toHaveBeenNthCalledWith(2, '/payment/public/orders/verify', { out_trade_no: 'order' }, { signal })
    expect(post).toHaveBeenNthCalledWith(3, '/payment/public/orders/resolve', { resume_token: 'token' }, { signal })
  })

})
