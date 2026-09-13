// Run from frontend: node src/views/admin/__tests__/groupsPricingBrowser.mjs
// An isolated real GroupsView, with HTTP fixtures only; no upstream requests.
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { resolve } from 'node:path'
import { isDeepStrictEqual } from 'node:util'

const payloads = []
let fixture
const server = await createServer({
  configFile: false,
  root: process.cwd(),
  plugins: [vue(), {
    name: 'group-pricing-http-fixture',
    configureServer(server) {
      server.middlewares.use(async (request, response, next) => {
        const path = request.url.split('?')[0]
        if (path === '/admin/groups') {
          response.setHeader('Content-Type', 'text/html')
          response.end(await server.transformIndexHtml(path, `<html><body><div id="app"></div><script type="module">
            import { createApp } from 'vue';
            import { createPinia } from 'pinia';
            import { createRouter, createWebHistory } from 'vue-router';
            import GroupsView from '/src/views/admin/GroupsView.vue';
            import i18n, { initI18n } from '/src/i18n/index.ts';
            await initI18n();
            import '/src/style.css';
            const app = createApp(GroupsView);
            app.use(createPinia()).use(i18n).use(createRouter({history:createWebHistory(),routes:[{path:'/admin/groups',component:GroupsView}]})).mount('#app');
          </script></body></html>`))
          return
        }
        if (path === '/__pricing-results') {
          const cards = fixture ? [fixture.complexCard, fixture.simpleCard] : []
          response.setHeader('Content-Type', 'application/json')
          response.end(JSON.stringify({
            payloads,
            renamePreserved: payloads.some(payload => payload.name === 'Browser rename' && isDeepStrictEqual(payload.model_pricing, cards)),
            providerDeletePreserved: payloads.some(payload => isDeepStrictEqual(payload.model_pricing, [fixture.simpleCard])),
            deleteAllPreserved: payloads.some(payload => isDeepStrictEqual(payload.model_pricing, [])),
          }))
          return
        }
        if (!path.startsWith('/api/')) return next()
        response.setHeader('Content-Type', 'application/json')
        const group = { ...fixture.sourceGroup, platform: 'composite', model_pricing: [fixture.complexCard, fixture.simpleCard] }
        let data = []
        if (request.method === 'PUT') {
          let body = ''
          for await (const chunk of request) body += chunk
          payloads.push(JSON.parse(body))
          data = group
        } else if (path === '/api/v1/admin/groups') {
          data = { items: [group], total: 1, page: 1, page_size: 20, pages: 1 }
        } else if (path.endsWith('/live-capability')) {
          data = { supported: false }
        } else if (path.endsWith('/settings/public')) {
          data = {}
        }
        response.end(JSON.stringify({ code: 0, data }))
      })
    },
  }],
  resolve: { alias: { '@': resolve('src') } },
  optimizeDeps: { include: ['vue', 'pinia', 'vue-router'] },
  server: { host: '127.0.0.1', port: 5198, strictPort: true },
})
fixture = await server.ssrLoadModule('/src/views/admin/__tests__/groupsPricingFixture.ts')
await server.listen()
console.log('Pricing fixture: http://127.0.0.1:5198/admin/groups')
