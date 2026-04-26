<script setup lang="ts">
import { computed } from "vue";
import AppIcon from "@/components/AppIcon.vue";
import { useUndoStore } from "@/stores/undo";

const undo = useUndoStore();
const toast = computed(() => undo.toast);
</script>

<template>
  <Teleport to="body">
    <Transition name="undo-toast">
      <aside v-if="toast" class="undo-toast">
        <div class="undo-toast__copy">
          <strong>{{ toast.title }}</strong>
          <span>{{ toast.detail }}</span>
        </div>
        <div class="undo-toast__actions">
          <button
            type="button"
            class="btn btn-ghost btn-sm"
            @click="void undo.actToast()"
          >
            <AppIcon
              :name="toast.mode === 'undo' ? 'undo-2' : 'redo-2'"
              :size="12"
            />
            {{ toast.mode === "undo" ? "Undo" : "Redo" }}
          </button>
          <button
            type="button"
            class="undo-toast__close"
            @click="undo.dismissToast()"
          >
            ×
          </button>
        </div>
      </aside>
    </Transition>
  </Teleport>
</template>

<style scoped>
.undo-toast {
  position: fixed;
  right: 20px;
  bottom: 20px;
  z-index: 390;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.8rem;
  width: min(420px, calc(100vw - 32px));
  padding: 0.95rem 1rem;
  border-radius: 18px;
  border: 1px solid rgba(46, 109, 164, 0.15);
  background:
    radial-gradient(
      circle at top right,
      rgba(46, 109, 164, 0.16),
      transparent 38%
    ),
    linear-gradient(
      180deg,
      rgba(255, 255, 255, 0.98),
      rgba(242, 245, 248, 0.98)
    );
  box-shadow: 0 18px 44px rgba(26, 38, 54, 0.16);
}
.undo-toast__copy {
  display: flex;
  flex-direction: column;
  gap: 0.12rem;
}
.undo-toast__copy strong {
  font-family: "Bricolage Grotesque", serif;
  font-size: 1rem;
}
.undo-toast__copy span {
  color: var(--text-muted);
  font-size: 13px;
}
.undo-toast__actions {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}
.undo-toast__close {
  border: 0;
  background: transparent;
  color: var(--text-muted);
  font-size: 18px;
}
.undo-toast-enter-active,
.undo-toast-leave-active {
  transition:
    opacity 0.2s ease,
    transform 0.2s cubic-bezier(0.2, 0.7, 0.1, 1);
}
.undo-toast-enter-from,
.undo-toast-leave-to {
  opacity: 0;
  transform: translateY(10px) scale(0.98);
}
</style>
