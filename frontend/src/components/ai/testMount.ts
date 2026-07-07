import { createApp, h, nextTick, type Component } from 'vue'
import { getActivePinia } from 'pinia'
import i18n from '@/i18n'

export async function mountComponent(component: Component, props: Record<string, unknown> = {}, slots: Record<string, any> = {}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({
    render: () => h(component as any, props, slots),
  })
  const activePinia = getActivePinia()
  if (activePinia) app.use(activePinia)
  app.use(i18n)
  app.mount(el)
  await nextTick()
  return {
    el,
    app,
    async unmount() {
      app.unmount()
      el.remove()
      await nextTick()
    },
  }
}
