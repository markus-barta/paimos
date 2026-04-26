<script setup lang="ts">
import { ref } from 'vue'
import AppModal from '@/components/AppModal.vue'
import { ApiError, errMsg } from '@/api/client'
import { completeEpic, loadIssueChildren } from '@/services/issueEpicCompletion'
import type { Issue } from '@/types'

const props = defineProps<{
  issueId: number
  issueKey: string
  children: Issue[]
}>()

const emit = defineEmits<{
  (e: 'completed', issue: Issue, children: Issue[]): void
}>()

const open          = ref(false)
const force         = ref(false)
const openCount     = ref(0)
const saving        = ref(false)
const error         = ref('')

function show() {
  force.value  = false
  openCount.value  = 0
  error.value  = ''
  open.value   = true
}

async function markDone() {
  error.value  = ''
  saving.value = true
  try {
    const updated = await completeEpic(props.issueId, force.value)
    const ch = await loadIssueChildren(props.issueId).catch(() => [])
    open.value = false
    emit('completed', updated, ch)
  } catch (e: unknown) {
    if (e instanceof ApiError && e.status === 422) {
      openCount.value = props.children.filter(
        c => c.status !== 'done' && c.status !== 'cancelled'
      ).length
    } else {
      error.value = errMsg(e, 'Failed.')
    }
  } finally {
    saving.value = false
  }
}

defineExpose({ show })
</script>

<template>
  <AppModal title="Complete Epic" :open="open" @close="open = false"
    :confirm-key="openCount === 0 ? 'o' : 'l'"
    @confirm="openCount === 0 ? markDone() : (force = true, markDone())">
    <div v-if="openCount === 0">
      <p>Mark <strong>{{ issueKey }}</strong> as done?</p>
      <p v-if="error" class="form-error">{{ error }}</p>
      <div class="modal-actions">
        <button class="btn btn-ghost btn-sm" @click="open = false"><u>C</u>ancel</button>
        <button class="btn btn-primary btn-sm" :disabled="saving" @click="markDone">
          <template v-if="saving">Saving…</template><template v-else>C<u>o</u>nfirm</template>
        </button>
      </div>
    </div>
    <div v-else>
      <p>
        <strong>{{ openCount }}</strong>
        child ticket{{ openCount !== 1 ? 's are' : ' is' }} still open.
        Close {{ openCount !== 1 ? 'them all' : 'it' }} and complete this epic?
      </p>
      <p v-if="error" class="form-error">{{ error }}</p>
      <div class="modal-actions">
        <button class="btn btn-ghost btn-sm" @click="open = false"><u>C</u>ancel</button>
        <button class="btn btn-danger btn-sm" :disabled="saving" @click="force = true; markDone()">
          <template v-if="saving">Closing…</template><template v-else>C<u>l</u>ose All &amp; Complete</template>
        </button>
      </div>
    </div>
  </AppModal>
</template>

<style scoped>
.form-error { font-size: 13px; color: #c0392b; background: #fde8e8; padding: .5rem .75rem; border-radius: var(--radius); }
.modal-actions { display: flex; justify-content: flex-end; gap: .5rem; margin-top: 1.25rem; }
</style>
