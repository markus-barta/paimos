import { afterEach, describe, expect, it } from 'vitest'
import { createApp, nextTick } from 'vue'
import TagChip from '@/components/TagChip.vue'

async function mountTag(name: string) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp(TagChip, {
    tag: { id: 1, name, color: 'blue', description: '', system: name === 'CUSTOMERPORTAL', created_at: '' },
  })
  app.mount(el)
  await nextTick()
  return {
    el,
    unmount() {
      app.unmount()
      el.remove()
    },
  }
}

describe('TagChip', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('renders CUSTOMERPORTAL as a compact CP marker with tooltip', async () => {
    const mounted = await mountTag('CUSTOMERPORTAL')
    const chip = mounted.el.querySelector('.tag-chip')

    expect(chip?.textContent?.trim()).toBe('CP')
    expect(chip?.getAttribute('title')).toBe('issue is shown in customer portal')
    expect(chip?.querySelector('svg')).not.toBeNull()

    mounted.unmount()
  })

  it('keeps normal tag labels unchanged', async () => {
    const mounted = await mountTag('backend')
    const chip = mounted.el.querySelector('.tag-chip')

    expect(chip?.textContent?.trim()).toBe('backend')
    expect(chip?.getAttribute('title')).toBeNull()

    mounted.unmount()
  })
})
