<script setup lang="ts">
import AppIcon from '@/components/AppIcon.vue'
import type { SavedView } from '@/types'

type EpicMode = 'key' | 'title' | 'abbreviated'

defineProps<{
  // Views data
  myViews: SavedView[]
  basicsViews: SavedView[]
  sharedViews: SavedView[]
  activeViewId: number | null
  viewIsModified: boolean
  isAdmin: boolean
  // Epic display mode
  epicDisplayMode: EpicMode
}>()

const emit = defineEmits<{
  applyView: [view: SavedView]
  openSaveView: []
  openEditView: [view: SavedView]
  deleteView: [view: SavedView]
  copyView: [view: SavedView]
  pinView: [id: number]
  unpinView: [id: number]
  updateCurrentView: []
  setEpicMode: [mode: EpicMode]
}>()
</script>

<template>
  <div class="views-panel">
    <div class="vp-header">
      <span class="fp-title">Saved views</span>
      <button class="fp-clear" @click="emit('openSaveView')">Save current</button>
    </div>

    <div v-if="viewIsModified && activeViewId !== null && activeViewId >= 0" class="vp-modified-banner">
      <span class="vp-modified-dot">&#8226;</span>
      <span class="vp-modified-label">Unsaved changes</span>
      <button class="vp-modified-btn" @click="emit('updateCurrentView')">Update view</button>
    </div>

    <template v-if="basicsViews.length > 0">
      <div class="vp-section-label">
        <span>Defaults</span>
        <span>{{ basicsViews.length }}</span>
      </div>
      <div
        v-for="v in basicsViews" :key="v.id"
        :class="['vp-row', { 'vp-row--active': activeViewId === v.id, 'vp-row--hidden': v.hidden }]"
        @click="emit('applyView', v)"
      >
        <AppIcon v-if="activeViewId === v.id" name="check" :size="13" class="vp-check" />
        <span class="vp-dot vp-dot--basics"></span>
        <div class="vp-row-body">
          <span class="vp-row-title">{{ v.title }}</span>
          <span v-if="v.description" class="vp-row-desc">{{ v.description }}</span>
        </div>
        <div class="vp-row-actions" @click.stop>
          <button
            :class="['vp-act', { 'vp-act--pinned': v.pinned }]"
            :title="v.pinned ? 'Unpin view' : 'Pin view'"
            @click="v.pinned ? emit('unpinView', v.id) : emit('pinView', v.id)"
          >
            <AppIcon :name="v.pinned ? 'pin' : 'pin-off'" :size="11" />
          </button>
          <button v-if="isAdmin" class="vp-act" @click="emit('openEditView', v)" title="Edit"><AppIcon name="pencil" :size="11" /></button>
          <button class="vp-act" @click="emit('copyView', v)" title="Copy to my views"><AppIcon name="copy-plus" :size="11" /></button>
        </div>
      </div>
    </template>

    <template v-if="myViews.length > 0 || basicsViews.length === 0">
      <div :class="['vp-section-label', { 'vp-section-label--gap': basicsViews.length > 0 }]">
        <span>My views</span>
        <span>{{ myViews.length }}</span>
      </div>
      <div v-if="myViews.length === 0" class="vp-empty">No saved views yet.</div>
      <div
        v-for="v in myViews" :key="v.id"
        :class="['vp-row', { 'vp-row--active': activeViewId === v.id }]"
        @click="emit('applyView', v)"
      >
        <AppIcon v-if="activeViewId === v.id" name="check" :size="13" class="vp-check" />
        <span class="vp-dot vp-dot--mine"></span>
        <div class="vp-row-body">
          <span class="vp-row-title">{{ v.title }}</span>
          <span v-if="v.description" class="vp-row-desc">{{ v.description }}</span>
        </div>
        <span v-if="v.is_admin_default" class="vp-pill vp-pill--basics">default</span>
        <span v-else-if="v.is_shared" class="vp-pill vp-pill--shared">shared</span>
        <div class="vp-row-actions" @click.stop>
          <button :class="['vp-act', { 'vp-act--pinned': v.pinned }]" :title="v.pinned ? 'Unpin view' : 'Pin view'" @click="v.pinned ? emit('unpinView', v.id) : emit('pinView', v.id)">
            <AppIcon :name="v.pinned ? 'pin' : 'pin-off'" :size="11" />
          </button>
          <button class="vp-act" @click="emit('openEditView', v)" title="Edit"><AppIcon name="pencil" :size="11" /></button>
          <button class="vp-act vp-act--danger" @click="emit('deleteView', v)" title="Delete"><AppIcon name="trash-2" :size="11" /></button>
        </div>
      </div>
    </template>

    <template v-if="sharedViews.length > 0">
      <div :class="['vp-section-label', { 'vp-section-label--gap': myViews.length > 0 || basicsViews.length > 0 }]">
        <span>Shared</span>
        <span>{{ sharedViews.length }}</span>
      </div>
      <div
        v-for="v in sharedViews" :key="v.id"
        :class="['vp-row', { 'vp-row--active': activeViewId === v.id }]"
        @click="emit('applyView', v)"
      >
        <AppIcon v-if="activeViewId === v.id" name="check" :size="13" class="vp-check" />
        <span class="vp-dot vp-dot--shared"></span>
        <div class="vp-row-body">
          <span class="vp-row-title">{{ v.title }}</span>
          <span v-if="v.description" class="vp-row-desc">{{ v.description }}</span>
        </div>
        <span class="vp-owner">{{ v.owner_username }}</span>
        <div class="vp-row-actions" @click.stop>
          <button class="vp-act" @click="emit('copyView', v)" title="Copy to my views"><AppIcon name="copy-plus" :size="11" /></button>
        </div>
      </div>
    </template>

    <div class="vp-section-label vp-section-label--gap vp-section-label--display">
      <span>Display</span>
    </div>
    <div class="vp-display-option">
      <span class="vp-display-label">Epic column</span>
      <div class="vp-display-toggle">
        <button v-for="m in (['key', 'title', 'abbreviated'] as const)" :key="m"
          :class="['vp-toggle-btn', { active: epicDisplayMode === m }]"
          @click="emit('setEpicMode', m)"
        >{{ m === 'abbreviated' ? 'Short' : m.charAt(0).toUpperCase() + m.slice(1) }}</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.views-panel {
  margin-top: .55rem; margin-bottom: 1rem;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: 8px; box-shadow: var(--shadow);
  padding: .7rem;
  width: min(26rem, 100%);
  max-width: calc(100vw - 3rem);
}
.vp-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: .65rem; }
.fp-title { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .07em; color: var(--text-muted); }
.fp-clear { background: none; border: none; font-size: 12px; color: var(--bp-blue); cursor: pointer; padding: 0; font-family: inherit; }
.fp-clear:hover { text-decoration: underline; }

.vp-modified-banner {
  display: flex; align-items: center; gap: .5rem;
  padding: .45rem .6rem; margin-bottom: .65rem;
  background: color-mix(in srgb, #f59e0b 8%, transparent);
  border: 1px solid color-mix(in srgb, #f59e0b 25%, transparent);
  border-radius: 6px; font-size: 12px;
}
.vp-modified-dot { color: #f59e0b; font-size: 16px; line-height: 1; margin-top: -1px; }
.vp-modified-label { color: var(--bp-blue-dark); font-weight: 500; flex: 1; }
.vp-modified-btn {
  background: none; border: none; padding: 0; cursor: pointer;
  color: var(--bp-blue-dark); font-weight: 600; font-size: 12px;
  font-family: inherit; text-decoration: underline; text-underline-offset: 2px;
}
.vp-modified-btn:hover { color: var(--bp-blue); }

.vp-section-label {
  display: flex; align-items: center; justify-content: space-between; gap: .75rem;
  font-size: 10px; font-weight: 700; text-transform: uppercase;
  letter-spacing: .07em; color: var(--text-muted);
  padding: .1rem 0 .3rem;
}
.vp-section-label--gap { margin-top: .75rem; }
.vp-section-label--display {
  border-top: 1px solid var(--border);
  padding-top: .65rem;
}

.vp-display-option { display: flex; align-items: center; justify-content: space-between; padding: .4rem 0; }
.vp-display-label { font-size: 12px; color: var(--text); font-weight: 500; }
.vp-display-toggle { display: flex; border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
.vp-toggle-btn { background: var(--bg); border: none; padding: .25rem .55rem; font-size: 11px; font-weight: 500; color: var(--text-muted); cursor: pointer; transition: background .12s, color .12s; }
.vp-toggle-btn + .vp-toggle-btn { border-left: 1px solid var(--border); }
.vp-toggle-btn.active { background: var(--bp-blue); color: #fff; }

.vp-empty { font-size: 12px; color: var(--text-muted); line-height: 1.5; padding: .4rem 0 .2rem; }

.vp-row {
  display: flex; align-items: center; gap: .6rem;
  padding: .42rem .5rem .42rem 1.85rem; border-radius: 6px; cursor: pointer;
  transition: background .1s; position: relative;
}
.vp-row:hover { background: #f0f2f5; }
.vp-row--active { background: var(--bp-blue-pale); }
.vp-row--active:hover { background: #d5e8f8; }
.vp-row--hidden { opacity: .5; }
.vp-check {
  position: absolute;
  left: .48rem;
  color: var(--bp-blue-dark);
}

.vp-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
.vp-dot--mine   { background: var(--bp-blue); }
.vp-dot--basics { background: #7c3aed; }
.vp-dot--shared { background: #059669; }

.vp-row-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: .05rem; }
.vp-row-title {
  font-size: 13px; font-weight: 500; color: var(--text);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; line-height: 1.3;
}
.vp-row--active .vp-row-title { color: var(--bp-blue-dark); font-weight: 600; }
.vp-row-desc {
  font-size: 11px; color: var(--text-muted); line-height: 1.3;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.vp-pill {
  font-size: 9px; font-weight: 700; text-transform: uppercase; letter-spacing: .05em;
  border-radius: 20px; padding: .15rem .45rem; flex-shrink: 0; line-height: 1.4;
}
.vp-pill--basics { background: #ede9fe; color: #6d28d9; }
.vp-pill--shared { background: #d1fae5; color: #065f46; }

.vp-owner {
  font-size: 11px; color: var(--text-muted); flex-shrink: 0;
  max-width: 80px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}

.vp-row-actions { display: flex; gap: .1rem; flex-shrink: 0; }
.vp-act {
  background: none; border: none; padding: .2rem .22rem;
  cursor: pointer; color: var(--text-muted); border-radius: 4px;
  display: inline-flex; align-items: center; font-family: inherit;
  transition: background .1s, color .1s, opacity .12s;
  opacity: 0;
}
.vp-row:hover .vp-act,
.vp-row--active .vp-act { opacity: 1; }
.vp-act--pinned { opacity: 1; }
.vp-act:hover { background: rgba(0,0,0,.06); color: var(--text); }
.vp-act--danger:hover { background: #fee2e2; color: #b91c1c; }
</style>
