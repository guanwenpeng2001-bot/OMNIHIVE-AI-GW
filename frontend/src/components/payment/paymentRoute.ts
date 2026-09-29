import { defineComponent, h, ref, watch, type Component } from 'vue'
import { useRoute, type RouteLocationNormalizedLoaded } from 'vue-router'

type PaymentPage = 'result' | 'stripe' | 'airwallex'

/** Private comparison value only: credentials must never become a VNode/DOM key. */
export function paymentPageIdentity(route: RouteLocationNormalizedLoaded, page: PaymentPage): string {
  const queryString = (key: string) => {
    const value = route.query[key]
    return (Array.isArray(value) ? value[0] : value) || ''
  }
  const identity = [route.path, queryString('order_id'), queryString('resume_token')]
  if (page === 'stripe') {
    identity.push(queryString('client_secret'), queryString('method'))
  } else {
    identity.push(queryString('out_trade_no'))
    // The presence of a legacy return marker enables public order verification.
    if (page === 'result') identity.push(String(!!queryString('trade_status').trim()))
  }
  return JSON.stringify(identity)
}

/** Each order owns a fresh setup scope, controller, SDK state and timers. */
export function paymentRouteComponent(component: Component, page: PaymentPage) {
  return defineComponent({
    name: 'PaymentRoute',
    setup() {
      const route = useRoute()
      const generation = ref(0)
      watch(() => paymentPageIdentity(route, page), () => { generation.value += 1 }, { flush: 'sync' })
      return () => h(component, { key: generation.value })
    },
  })
}
