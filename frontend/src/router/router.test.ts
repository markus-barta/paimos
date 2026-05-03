import { describe, expect, it } from 'vitest'

import router from './index'

describe('router meta', () => {
  // PAI-274: IssueList table relies on AppLayout's `.view-body--self-scroll`
  // variant to keep the sticky thead + frozen columns working. If this
  // meta drifts on any route that embeds IssueList, the regression returns
  // silently — past fixes were repeatedly papered over without a check,
  // hence this guard. Extend the array below whenever a new route gains
  // an internally-scrolling list (search results, custom views, …).
  const SELF_SCROLL_ROUTES = ['/issues', '/projects/:id']

  it.each(SELF_SCROLL_ROUTES)(
    'sets scrollMode=self on %s so AppLayout flex-bounds .view-body',
    (path) => {
      const r = router.getRoutes().find((r) => r.path === path)
      expect(r, `expected ${path} route to be registered`).toBeDefined()
      expect(r!.meta.scrollMode).toBe('self')
    },
  )

  it('leaves scrollMode unset on default page-scroll views (Settings, Customers, IssueDetail)', () => {
    // IssueDetail uses IssueList in `compact` mode (overflow:hidden, no
    // internal scroll), so its sticky thead inherits .main-content's
    // page-scroll context — opt-in is unnecessary and would clip content.
    for (const path of [
      '/settings',
      '/customers',
      '/projects/:id/issues/:issueId',
      '/issues/:issueId',
    ]) {
      const r = router.getRoutes().find((rt) => rt.path === path)
      expect(r?.meta.scrollMode, `${path} should be page-scroll`).toBeUndefined()
    }
  })

  it('registers direct issue detail deeplinks', () => {
    const r = router.getRoutes().find((rt) => rt.path === '/issues/:issueId')
    expect(r, 'expected /issues/:issueId route to be registered').toBeDefined()
  })
})
