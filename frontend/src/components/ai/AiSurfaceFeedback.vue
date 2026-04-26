<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AiActivityStrip from '@/components/ai/AiActivityStrip.vue'
import AiActionResultModal from '@/components/ai/AiActionResultModal.vue'
import AiOptimizeOverlay from '@/components/ai/AiOptimizeOverlay.vue'
import AiResultStrip from '@/components/ai/AiResultStrip.vue'
import { useAiAction } from '@/composables/useAiAction'
import { useAiOptimize } from '@/composables/useAiOptimize'
import { summarizeAiResult } from '@/composables/useAiResultSummary'

interface ActionApplyArgs {
  action: string
  subAction?: string
  field: string
  fieldLabel: string
  issueId?: number
  body: unknown
  intent?: string
  selection?: number[]
  values?: Record<string, unknown>
}

const props = defineProps<{
  hostKey: string
  apply?: (info: ActionApplyArgs) => void | Promise<void>
}>()

const { t } = useI18n()
const aiAction = useAiAction()
const aiOptimize = useAiOptimize()

const actionActivity = computed(() => aiAction.activity.value?.hostKey === props.hostKey ? aiAction.activity.value : null)
const optimizeActivity = computed(() => aiOptimize.activity.value?.hostKey === props.hostKey ? aiOptimize.activity.value : null)
const actionResult = computed(() => aiAction.result.value?.hostKey === props.hostKey ? aiAction.result.value : null)
const optimizeOverlay = computed(() => aiOptimize.overlay.visible && aiOptimize.overlay.hostKey === props.hostKey ? aiOptimize.overlay : null)
const errorMessage = computed(() => {
  if (aiAction.lastErrorHostKey.value === props.hostKey && aiAction.lastError.value) return aiAction.lastError.value
  if (aiOptimize.lastErrorHostKey.value === props.hostKey && aiOptimize.lastError.value) return aiOptimize.lastError.value
  return ''
})

const resultSummary = computed(() => {
  if (!actionResult.value) return ''
  return summarizeAiResult({
    action: actionResult.value.action,
    body: actionResult.value.body,
    sourceText: actionResult.value.sourceText,
  })
})

const actionDecision = computed(() => {
  const r = actionResult.value
  if (!r || !props.apply) return null
  if (r.action === 'find_parent' && Array.isArray((r.body as any)?.candidates) && (r.body as any).candidates.length > 0) {
    const top = (r.body as any).candidates[0]
    return {
      copy: t('ai.setAsParent', { issueKey: top.issue_key }),
      primary: {
        label: t('ai.apply'),
        action: () => props.apply?.({
          action: r.action,
          subAction: r.subAction,
          field: r.field,
          fieldLabel: r.fieldLabel,
          issueId: r.issueId,
          body: r.body,
          intent: 'move-under',
          values: { issue_key: top.issue_key },
        }),
      },
    }
  }
  if (r.action === 'estimate_effort') {
    return {
      copy: t('ai.applyEstimate'),
      primary: {
        label: t('ai.apply'),
        action: () => props.apply?.({
          action: r.action,
          subAction: r.subAction,
          field: r.field,
          fieldLabel: r.fieldLabel,
          issueId: r.issueId,
          body: r.body,
          intent: 'apply-estimate',
          values: { hours: (r.body as any)?.hours, lp: (r.body as any)?.lp },
        }),
      },
    }
  }
  return null
})

function clearError() {
  aiAction.clearError()
  aiOptimize.clearError()
}
</script>

<template>
  <div class="ai-surface-feedback">
    <AiActivityStrip
      v-if="actionActivity"
      :action-key="actionActivity.action"
      :title="t('ai.workingTitle', { action: actionActivity.fieldLabel || actionActivity.action })"
      :started-at="actionActivity.startedAt"
      :cancellable="false"
    />
    <AiActivityStrip
      v-else-if="optimizeActivity"
      :action-key="optimizeActivity.actionKey"
      :title="t('ai.workingTitle', { action: optimizeActivity.fieldLabel || optimizeActivity.actionKey })"
      :started-at="optimizeActivity.startedAt"
      :cancellable="false"
    />

    <div v-if="errorMessage" class="ai-surface-error" role="alert">
      <span>{{ t('ai.failedPrefix') }}: {{ errorMessage }}</span>
      <button type="button" class="ai-surface-error__btn" @click="clearError">×</button>
    </div>

    <AiResultStrip
      v-if="actionResult"
      :action-key="actionResult.action"
      :title="t('ai.resultTitle', { action: actionResult.fieldLabel || actionResult.action })"
      :summary="resultSummary"
      :details-label="t('ai.details')"
      :primary="actionDecision?.primary"
      :dismissable="true"
      :auto-dismiss-ms="actionDecision ? undefined : 12000"
      @dismiss="aiAction.reset()"
    >
      <template v-if="actionDecision" #decision>
        {{ actionDecision.copy }}
      </template>
      <div class="ai-surface-detail">
        <div class="ai-surface-detail__meta">
          <span>{{ t('ai.modelLabel') }}: {{ actionResult.model || '—' }}</span>
          <span>{{ t('ai.tokensLabel') }}: {{ (actionResult.promptTokens ?? 0) + (actionResult.completionTokens ?? 0) }}</span>
        </div>
        <p>{{ t('ai.detailsHint') }}</p>
      </div>
    </AiResultStrip>

    <AiActionResultModal :host-key="hostKey" :apply="apply" />

    <AiOptimizeOverlay
      v-if="optimizeOverlay"
      :original="optimizeOverlay.original"
      :optimized="optimizeOverlay.optimized"
      :field-label="optimizeOverlay.fieldLabel"
      :model-name="optimizeOverlay.modelName"
      :retrying="optimizeOverlay.retrying"
      @accept="aiOptimize.accept()"
      @reject="aiOptimize.reject()"
      @retry="aiOptimize.retry()"
    />
  </div>
</template>

<style scoped>
.ai-surface-feedback {
  display: flex;
  flex-direction: column;
  gap: .5rem;
}
.ai-surface-error {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: .75rem;
  padding: .65rem .8rem;
  border: 1px solid rgba(192, 57, 43, .24);
  border-radius: 10px;
  background: rgba(253, 242, 242, .95);
  color: #8f2d23;
  font-size: 13px;
}
.ai-surface-error__btn {
  border: none;
  background: transparent;
  color: inherit;
  font-size: 18px;
}
.ai-surface-detail__meta {
  display: flex;
  gap: .75rem;
  flex-wrap: wrap;
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 11px;
  color: var(--text-muted);
  margin-bottom: .35rem;
}
.ai-surface-detail p {
  font-size: 12px;
  color: var(--text-muted);
}
</style>
