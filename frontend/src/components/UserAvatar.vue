<script setup lang="ts">
import { ref, computed } from 'vue'

const props = withDefaults(defineProps<{
  user: { username: string; avatar_path?: string; first_name?: string; last_name?: string; email?: string; nickname?: string } | null | undefined
  size?: 'sm' | 'md' | 'lg'
  showTooltip?: boolean
}>(), {
  size: 'sm',
  showTooltip: false,
})

const imgError      = ref(false)
const hovered       = ref(false)
const tooltipVisible = ref(false)
let tooltipTimer: ReturnType<typeof setTimeout> | null = null

const initials = computed(() => {
  if (!props.user) return '?'
  const src = props.user.nickname?.trim() || props.user.username
  return src.slice(0, 3).toUpperCase()
})

const displayName = computed(() => {
  if (!props.user) return ''
  const fn = props.user.first_name?.trim()
  const ln = props.user.last_name?.trim()
  return props.user.nickname?.trim() || [fn, ln].filter(Boolean).join(' ') || props.user.username
})

const hasAvatar = computed(() => !!props.user?.avatar_path && !imgError.value)

function onImgError() { imgError.value = true }

function onMouseEnter() {
  hovered.value = true
  if (!props.showTooltip) return
  tooltipTimer = setTimeout(() => { tooltipVisible.value = true }, 300)
}
function onMouseLeave() {
  hovered.value = false
  if (tooltipTimer) { clearTimeout(tooltipTimer); tooltipTimer = null }
  tooltipVisible.value = false
}
</script>

<template>
  <span
    class="ua"
    :class="[`ua--${size}`, { 'ua--empty': !user }]"
    @mouseenter="onMouseEnter"
    @mouseleave="onMouseLeave"
  >
    <img
      v-if="hasAvatar"
      :src="user!.avatar_path"
      :alt="user!.username"
      class="ua-img"
      loading="lazy"
      @error="onImgError"
    />
    <span v-else class="ua-initials">{{ initials }}</span>

    <!-- Hover overlay for image avatars -->
    <span v-if="hasAvatar && hovered" class="ua-hover-overlay">{{ initials }}</span>

    <span v-if="showTooltip && tooltipVisible && user" class="ua-tooltip">
      <span class="ua-tooltip-avatar">
        <img v-if="hasAvatar" :src="user.avatar_path" class="ua-tooltip-img" loading="lazy" />
        <span v-else class="ua-tooltip-initials">{{ initials }}</span>
      </span>
      <span class="ua-tooltip-info">
        <span class="ua-tooltip-name">{{ displayName }}</span>
        <span class="ua-tooltip-username">@{{ user.username }}</span>
        <span v-if="user.email" class="ua-tooltip-email">{{ user.email }}</span>
      </span>
    </span>
  </span>
</template>

<style scoped>
.ua {
  display: inline-flex; align-items: center; justify-content: center;
  border-radius: 50%;
  background: var(--bp-blue-pale); color: var(--bp-blue-dark);
  font-weight: 700; flex-shrink: 0; overflow: hidden;
  position: relative;
}
.ua--sm { width: 20px; height: 20px; font-size: 5.5px; }
.ua--md { width: 24px; height: 24px; font-size: 6px; }
.ua--lg { width: 32px; height: 32px; font-size: 7px; }
.ua--empty { background: var(--border); color: var(--text-muted); }

.ua-img { width: 100%; height: 100%; object-fit: cover; border-radius: 50%; display: block; }
.ua-initials { line-height: 1; }

/* Hover overlay for image avatars */
.ua-hover-overlay {
  position: absolute; inset: 0;
  background: rgba(0, 0, 0, .35);
  color: #fff; font-size: inherit; font-weight: 700;
  display: flex; align-items: center; justify-content: center;
  border-radius: 50%;
  transition: opacity .15s;
  line-height: 1;
  pointer-events: none;
}

/* Tooltip — overflow: visible on the span so tooltip can escape */
.ua { overflow: visible; }
.ua-img, .ua-hover-overlay { overflow: hidden; border-radius: 50%; }
.ua-tooltip {
  position: absolute; bottom: calc(100% + 6px); left: 50%; transform: translateX(-50%);
  z-index: 200;
  display: flex; align-items: center; gap: .6rem;
  padding: .6rem .75rem;
  background: var(--bg-card); border: 1px solid var(--border);
  border-radius: var(--radius); box-shadow: var(--shadow-md);
  white-space: nowrap; pointer-events: none;
  min-width: 160px;
}
.ua-tooltip-avatar {
  flex-shrink: 0; width: 36px; height: 36px; border-radius: 50%; overflow: hidden;
  display: flex; align-items: center; justify-content: center;
  background: var(--bp-blue-pale);
}
.ua-tooltip-img { width: 100%; height: 100%; object-fit: cover; }
.ua-tooltip-initials {
  font-size: 13px; font-weight: 700; color: var(--bp-blue-dark);
}
.ua-tooltip-info { display: flex; flex-direction: column; gap: .1rem; }
.ua-tooltip-name     { font-size: 13px; font-weight: 600; color: var(--text); }
.ua-tooltip-username { font-size: 11px; color: var(--text-muted); }
.ua-tooltip-email    { font-size: 11px; color: var(--text-muted); }
</style>
