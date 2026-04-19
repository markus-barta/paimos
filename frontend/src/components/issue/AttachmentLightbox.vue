<script setup lang="ts">
/**
 * AttachmentLightbox — singleton modal mounted once in AppLayout.
 *
 * Renders the currently-open attachment from useAttachmentLightbox() with:
 *  - left/right navigation (keyboard + buttons)
 *  - scroll-wheel / +−0 zoom clamped to 1×–4×
 *  - download button (forces download via <a download>)
 *  - copy-reference popover (alignment × size → HTML img snippet)
 *  - escape / backdrop click to close
 */
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import {
  useAttachmentLightbox,
  buildMarkdownReference,
  type LightboxAlignment,
  type LightboxSize,
} from '@/composables/useAttachmentLightbox'

const lb = useAttachmentLightbox()

// Zoom + reset on navigation
const zoom = ref(1)
const MIN_ZOOM = 1
const MAX_ZOOM = 4
function zoomIn()    { zoom.value = Math.min(MAX_ZOOM, +(zoom.value + 0.25).toFixed(2)) }
function zoomOut()   { zoom.value = Math.max(MIN_ZOOM, +(zoom.value - 0.25).toFixed(2)) }
function zoomReset() { zoom.value = 1 }

function onWheel(e: WheelEvent) {
  e.preventDefault()
  if (e.deltaY < 0) zoomIn()
  else              zoomOut()
}

// Copy-reference popover state
const copyOpen = ref(false)
const pickedAlign = ref<LightboxAlignment>('center')
const pickedSize  = ref<LightboxSize>('md')
const copyFlash   = ref(false)

const ALIGNMENTS: Array<{ key: LightboxAlignment; label: string; icon: string }> = [
  { key: 'left',   label: 'Left',   icon: 'align-left'   },
  { key: 'center', label: 'Center', icon: 'align-center' },
  { key: 'right',  label: 'Right',  icon: 'align-right'  },
  { key: 'full',   label: 'Full',   icon: 'maximize-2'   },
]
const SIZES: Array<{ key: LightboxSize; label: string }> = [
  { key: 'sm',   label: 'S · 200' },
  { key: 'md',   label: 'M · 400' },
  { key: 'lg',   label: 'L · 600' },
  { key: 'full', label: 'Full'    },
]

async function copyReference() {
  const att = lb.current.value
  if (!att) return
  const snippet = buildMarkdownReference(att, pickedAlign.value, pickedSize.value)
  try {
    await navigator.clipboard.writeText(snippet)
    copyFlash.value = true
    setTimeout(() => { copyFlash.value = false }, 1400)
  } catch {
    // Clipboard API can fail in non-secure contexts — fall back to a hidden textarea.
    const ta = document.createElement('textarea')
    ta.value = snippet
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
    copyFlash.value = true
    setTimeout(() => { copyFlash.value = false }, 1400)
  }
}

// Download — forces a save via the `download` attribute on a synthetic <a>
function download() {
  const att = lb.current.value
  if (!att) return
  const a = document.createElement('a')
  a.href = `/api/attachments/${att.id}`
  a.download = att.filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
}

// Reset zoom + close the copy popover on every navigation so the next
// image starts fresh.
watch(() => lb.currentIndex.value, () => { zoom.value = 1; copyOpen.value = false })
watch(() => lb.open.value, (isOpen) => { if (!isOpen) { zoom.value = 1; copyOpen.value = false } })

// Global keyboard shortcuts while the lightbox is open.
function onKeydown(e: KeyboardEvent) {
  if (!lb.open.value) return
  switch (e.key) {
    case 'Escape': lb.close(); break
    case 'ArrowLeft': if (lb.canStep.value) { lb.prev(); e.preventDefault() } break
    case 'ArrowRight': if (lb.canStep.value) { lb.next(); e.preventDefault() } break
    case '+': case '=': zoomIn();  e.preventDefault(); break
    case '-': case '_': zoomOut(); e.preventDefault(); break
    case '0':          zoomReset(); e.preventDefault(); break
  }
}
window.addEventListener('keydown', onKeydown)
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))

const imageUrl = computed(() =>
  lb.current.value ? `/api/attachments/${lb.current.value.id}` : '',
)
</script>

<template>
  <Teleport to="body">
    <Transition name="lightbox-fade">
      <div
        v-if="lb.open.value && lb.current.value"
        class="lightbox"
        role="dialog"
        aria-modal="true"
        @click.self="lb.close()"
      >
        <!-- Header -->
        <header class="lb-header" @click.stop>
          <div class="lb-title">
            <AppIcon name="image" :size="14" />
            <span class="lb-filename" :title="lb.current.value.filename">{{ lb.current.value.filename }}</span>
            <span v-if="lb.canStep.value" class="lb-index">
              {{ lb.currentIndex.value + 1 }} / {{ lb.attachments.value.length }}
            </span>
          </div>
          <div class="lb-actions">
            <div class="lb-zoom-group">
              <button type="button" class="lb-btn" title="Zoom out (−)" @click="zoomOut" :disabled="zoom <= MIN_ZOOM">
                <AppIcon name="minus" :size="14" />
              </button>
              <span class="lb-zoom-value">{{ Math.round(zoom * 100) }}%</span>
              <button type="button" class="lb-btn" title="Zoom in (+)" @click="zoomIn" :disabled="zoom >= MAX_ZOOM">
                <AppIcon name="plus" :size="14" />
              </button>
              <button type="button" class="lb-btn" title="Reset zoom (0)" @click="zoomReset" :disabled="zoom === 1">
                <AppIcon name="maximize" :size="14" />
              </button>
            </div>
            <div class="lb-divider" />
            <div class="lb-copy-wrap">
              <button type="button" class="lb-btn" title="Copy markdown reference" @click="copyOpen = !copyOpen">
                <AppIcon name="link" :size="14" />
                <span class="lb-btn-label">Reference</span>
              </button>
              <div v-if="copyOpen" class="lb-copy-pop" @click.stop>
                <div class="lb-pop-label">Alignment</div>
                <div class="lb-pop-row">
                  <button
                    v-for="a in ALIGNMENTS" :key="a.key"
                    type="button"
                    class="lb-pop-btn"
                    :class="{ 'lb-pop-btn--active': pickedAlign === a.key }"
                    :title="a.label"
                    @click="pickedAlign = a.key"
                  >
                    <AppIcon :name="a.icon" :size="13" />
                  </button>
                </div>
                <div class="lb-pop-label">Size</div>
                <div class="lb-pop-row lb-pop-row--sizes">
                  <button
                    v-for="s in SIZES" :key="s.key"
                    type="button"
                    class="lb-pop-btn lb-pop-btn--size"
                    :class="{ 'lb-pop-btn--active': pickedSize === s.key }"
                    @click="pickedSize = s.key"
                  >
                    {{ s.label }}
                  </button>
                </div>
                <button type="button" class="lb-copy-btn" @click="copyReference">
                  <AppIcon :name="copyFlash ? 'check' : 'clipboard'" :size="13" />
                  {{ copyFlash ? 'Copied!' : 'Copy reference' }}
                </button>
              </div>
            </div>
            <button type="button" class="lb-btn" title="Download" @click="download">
              <AppIcon name="download" :size="14" />
              <span class="lb-btn-label">Download</span>
            </button>
            <div class="lb-divider" />
            <button type="button" class="lb-btn" title="Close (Esc)" @click="lb.close()">
              <AppIcon name="x" :size="16" />
            </button>
          </div>
        </header>

        <!-- Body -->
        <div class="lb-body" @click.self="lb.close()" @wheel="onWheel">
          <button
            v-if="lb.canStep.value"
            type="button"
            class="lb-nav lb-nav--left"
            title="Previous (←)"
            @click.stop="lb.prev()"
          >
            <AppIcon name="chevron-left" :size="28" />
          </button>

          <div class="lb-stage" @click.self="lb.close()">
            <img
              :src="imageUrl"
              :alt="lb.current.value.filename"
              class="lb-image"
              :class="{ 'lb-image--zoomed': zoom > 1 }"
              :style="{ transform: `scale(${zoom})` }"
              draggable="false"
              @click.stop
            />
          </div>

          <button
            v-if="lb.canStep.value"
            type="button"
            class="lb-nav lb-nav--right"
            title="Next (→)"
            @click.stop="lb.next()"
          >
            <AppIcon name="chevron-right" :size="28" />
          </button>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.lightbox {
  position: fixed;
  inset: 0;
  z-index: 10000;
  background: rgba(10, 14, 22, .92);
  display: flex;
  flex-direction: column;
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
}

.lb-header {
  display: flex;
  align-items: center;
  gap: 1rem;
  padding: .6rem 1rem;
  border-bottom: 1px solid rgba(255,255,255,.1);
  color: #fff;
  flex-shrink: 0;
}
.lb-title {
  display: flex;
  align-items: center;
  gap: .5rem;
  font-size: 13px;
  font-weight: 600;
  min-width: 0;
}
.lb-filename {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 360px;
}
.lb-index {
  font-size: 11px;
  color: rgba(255,255,255,.55);
  font-variant-numeric: tabular-nums;
  padding: 0 .4rem;
  border-left: 1px solid rgba(255,255,255,.15);
  margin-left: .5rem;
}
.lb-actions {
  display: flex;
  align-items: center;
  gap: .4rem;
  margin-left: auto;
}
.lb-divider {
  width: 1px;
  height: 20px;
  background: rgba(255,255,255,.15);
  margin: 0 .15rem;
}
.lb-btn {
  background: transparent;
  border: 1px solid rgba(255,255,255,.15);
  color: rgba(255,255,255,.85);
  padding: .35rem .55rem;
  border-radius: 4px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  font-size: 12px;
  font-family: inherit;
  transition: background .12s, border-color .12s, color .12s;
}
.lb-btn:hover:not(:disabled) {
  background: rgba(255,255,255,.08);
  border-color: rgba(255,255,255,.3);
  color: #fff;
}
.lb-btn:disabled { opacity: .35; cursor: default; }
.lb-btn-label { font-size: 11px; letter-spacing: .02em; }
.lb-zoom-group {
  display: inline-flex;
  align-items: center;
  gap: .2rem;
}
.lb-zoom-value {
  font-size: 11px;
  color: rgba(255,255,255,.65);
  font-variant-numeric: tabular-nums;
  min-width: 42px;
  text-align: center;
}

/* Copy reference popover */
.lb-copy-wrap { position: relative; }
.lb-copy-pop {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  background: #1a1f28;
  border: 1px solid rgba(255,255,255,.15);
  border-radius: 6px;
  padding: .6rem .6rem .55rem;
  min-width: 220px;
  box-shadow: 0 8px 24px rgba(0,0,0,.45);
  display: flex;
  flex-direction: column;
  gap: .35rem;
  z-index: 1;
  animation: lb-pop-in 140ms cubic-bezier(.2,.7,.2,1);
}
@keyframes lb-pop-in {
  from { opacity: 0; transform: translateY(-4px); }
  to   { opacity: 1; transform: translateY(0); }
}
.lb-pop-label {
  font-size: 9px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: .08em;
  color: rgba(255,255,255,.45);
  margin-top: .1rem;
}
.lb-pop-row {
  display: flex;
  gap: .25rem;
}
.lb-pop-row--sizes {
  flex-wrap: wrap;
}
.lb-pop-btn {
  background: rgba(255,255,255,.04);
  border: 1px solid rgba(255,255,255,.1);
  color: rgba(255,255,255,.75);
  padding: .35rem .45rem;
  border-radius: 4px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-family: inherit;
  font-size: 11px;
  flex: 1 1 auto;
  transition: background .12s, border-color .12s, color .12s;
}
.lb-pop-btn:hover {
  background: rgba(255,255,255,.1);
  color: #fff;
}
.lb-pop-btn--active {
  background: rgba(46,109,164,.25);
  border-color: rgba(46,109,164,.6);
  color: #fff;
}
.lb-pop-btn--size {
  min-width: 56px;
  font-variant-numeric: tabular-nums;
}
.lb-copy-btn {
  margin-top: .3rem;
  background: var(--bp-blue, #2e6da4);
  border: 1px solid var(--bp-blue, #2e6da4);
  color: #fff;
  padding: .45rem .7rem;
  border-radius: 4px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: .4rem;
  font-family: inherit;
  font-size: 12px;
  font-weight: 600;
  transition: background .12s, border-color .12s;
}
.lb-copy-btn:hover { background: #3d82be; }

/* Body + image stage */
.lb-body {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 0;
  position: relative;
  overflow: hidden;
}
.lb-stage {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  padding: 1rem;
  height: 100%;
  min-width: 0;
}
.lb-image {
  max-width: min(90vw, 1600px);
  max-height: calc(100vh - 120px);
  object-fit: contain;
  border-radius: 4px;
  box-shadow: 0 8px 40px rgba(0,0,0,.6);
  transition: transform .18s cubic-bezier(.2,.7,.2,1);
  user-select: none;
  display: block;
}
.lb-image--zoomed { cursor: zoom-out; }

.lb-nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  background: rgba(0,0,0,.35);
  border: 1px solid rgba(255,255,255,.1);
  color: rgba(255,255,255,.85);
  width: 44px;
  height: 44px;
  border-radius: 50%;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background .12s, border-color .12s, color .12s, transform .12s;
  z-index: 2;
}
.lb-nav:hover {
  background: rgba(0,0,0,.6);
  border-color: rgba(255,255,255,.3);
  color: #fff;
  transform: translateY(-50%) scale(1.05);
}
.lb-nav--left  { left: 1rem; }
.lb-nav--right { right: 1rem; }

.lightbox-fade-enter-active,
.lightbox-fade-leave-active { transition: opacity .18s ease; }
.lightbox-fade-enter-from,
.lightbox-fade-leave-to { opacity: 0; }
</style>
