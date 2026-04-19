<script setup lang="ts">
import { STATUS_DOT_STYLE } from '@/composables/useIssueDisplay'

const props = defineProps<{ status: string }>()

const dot = () => STATUS_DOT_STYLE[props.status] ?? { color: '#9ca3af', outline: true }
</script>

<template>
  <span
    class="status-dot-icon"
    :class="{
      'status-dot-icon--outline': dot().outline,
      'status-dot-icon--cancelled': status === 'cancelled',
      'status-dot-icon--unknown': !(status in STATUS_DOT_STYLE),
    }"
    :style="dot().outline ? { borderColor: dot().color } : { background: dot().color }"
  >
    <span v-if="!(status in STATUS_DOT_STYLE)" class="status-dot-icon__q">?</span>
  </span>
</template>

<style scoped>
.status-dot-icon {
  width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
  display: inline-block; position: relative; border: 2px solid transparent;
  vertical-align: middle;
}
.status-dot-icon--outline {
  background: transparent !important;
  border-style: solid;
}
.status-dot-icon--cancelled::after {
  content: ''; position: absolute;
  top: 50%; left: -1px; right: -1px; height: 1.5px;
  background: #6b7280; transform: rotate(-45deg);
}
.status-dot-icon--unknown {
  width: 10px; height: 10px;
}
.status-dot-icon__q {
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  font-size: 7px; font-weight: 700; line-height: 1; color: #9ca3af;
}
</style>
