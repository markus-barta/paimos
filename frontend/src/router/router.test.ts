import { describe, expect, it } from 'vitest'

import router from './index'

describe('router meta', () => {
  // PAI-274: IssueList table relies on AppLayout's `.view-body--self-scroll`
  // variant to keep the sticky thead + frozen columns working. If this
  // meta drifts, the regression returns silently — past fixes were
  // repeatedly papered over without a check, hence this guard.
  it('sets scrollMode=self on /issues so AppLayout flex-bounds .view-body', () => {
    const issues = router
      .getRoutes()
      .find((r) => r.path === '/issues')
    expect(issues, 'expected /issues route to be registered').toBeDefined()
    expect(issues!.meta.scrollMode).toBe('self')
  })

  it('leaves scrollMode unset on default page-scroll views (Settings, Customers)', () => {
    const settings = router.getRoutes().find((r) => r.path === '/settings')
    const customers = router.getRoutes().find((r) => r.path === '/customers')
    expect(settings?.meta.scrollMode).toBeUndefined()
    expect(customers?.meta.scrollMode).toBeUndefined()
  })
})
