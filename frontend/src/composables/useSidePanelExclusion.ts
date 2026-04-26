/**
 * Right-edge side panel mutual exclusion.
 *
 * The app has multiple panels that occupy the right edge of the screen
 * (issue side panel, project workspace dock, undo activity). Showing more
 * than one at a time was visually confusing and stacked z-indexes
 * unpredictably. This module is a tiny window-event broadcast so each
 * panel can announce when it opens, and every other panel can listen and
 * close itself — without any of them needing to know the others exist.
 */

const EVENT_NAME = 'paimos:open-side-panel'

export type SidePanelId = 'undo' | 'issue' | 'aux'

export function notifySidePanelOpened(id: SidePanelId): void {
  window.dispatchEvent(new CustomEvent(EVENT_NAME, { detail: { id } }))
}

export function onOtherSidePanelOpened(
  self: SidePanelId,
  close: () => void,
): () => void {
  const handler = (e: Event) => {
    const detail = (e as CustomEvent<{ id: SidePanelId }>).detail
    if (detail?.id && detail.id !== self) close()
  }
  window.addEventListener(EVENT_NAME, handler)
  return () => window.removeEventListener(EVENT_NAME, handler)
}
