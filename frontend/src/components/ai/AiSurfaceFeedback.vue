<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AiActivityStrip from '@/components/ai/AiActivityStrip.vue'
import AiActionResultModal from '@/components/ai/AiActionResultModal.vue'
import AiOptimizeOverlay from '@/components/ai/AiOptimizeOverlay.vue'
import AiResultStrip from '@/components/ai/AiResultStrip.vue'
import { useAiAction } from '@/composables/useAiAction'
import { useAiOptimize } from '@/composables/useAiOptimize'
import { summarizeAiResult } from '@/composables/useAiResultSummary'

interface ActionApplyArgs {
  requestId?: string
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

interface ActionApplyResult {
  undoLabel?: string
  undo?: () => void | Promise<void>
  undoAutoDismissMs?: number
}

const props = defineProps<{
  hostKey: string
  apply?: (info: ActionApplyArgs) => void | Promise<void> | ActionApplyResult | Promise<ActionApplyResult | void>
}>()

const { t } = useI18n()
const aiAction = useAiAction()
const aiOptimize = useAiOptimize()
const undoState = ref<ActionApplyResult | null>(null)
let undoTimer: number | null = null
const modalOpen = ref(false)
const undoDismissMs = ref(5000)

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
    optimizedText: (actionResult.value.body as any)?.optimized ?? (actionResult.value.body as any)?.optimized_text ?? '',
  })
})
const modalShapeAction = computed(() => {
  const action = actionResult.value?.action
  return action === 'suggest_enhancement' || action === 'spec_out' || action === 'generate_subtasks' || action === 'ui_generation'
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
        action: () => runApply({
          requestId: r.requestId,
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
      secondary: ((r.body as any)?.candidates ?? []).slice(1, 3).map((candidate: any) => ({
        label: candidate.issue_key,
        action: () => runApply({
          requestId: r.requestId,
          action: r.action,
          subAction: r.subAction,
          field: r.field,
          fieldLabel: r.fieldLabel,
          issueId: r.issueId,
          body: r.body,
          intent: 'move-under',
          values: { issue_key: candidate.issue_key },
        }),
      })),
      explain: {
        label: t('ai.details'),
        action: () => undefined,
      },
    }
  }
  if (r.action === 'estimate_effort') {
    return {
      copy: t('ai.applyEstimate'),
      primary: {
        label: t('ai.apply'),
        action: () => runApply({
          requestId: r.requestId,
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
      secondary: [
        {
          label: t('ai.dismiss'),
          action: () => aiAction.reset(),
        },
      ],
      explain: {
        label: t('ai.showReasoning'),
        action: () => undefined,
      },
    }
  }
  if (r.action === 'tone_check' && ((r.body as any)?.optimized || (r.body as any)?.optimized_text)) {
    const rewritten = (r.body as any).optimized ?? (r.body as any).optimized_text
    return {
      copy: t('ai.applyToneCheck'),
      primary: {
        label: t('ai.apply'),
        action: () => runApply({
          action: r.action,
          subAction: r.subAction,
          field: r.field,
          fieldLabel: r.fieldLabel,
          issueId: r.issueId,
          body: r.body,
          intent: 'replace-text',
          values: { text: rewritten },
        }),
      },
      secondary: [
        { label: t('ai.dismiss'), action: () => aiAction.reset() },
      ],
    }
  }
  if (r.action === 'detect_duplicates' && Array.isArray((r.body as any)?.matches) && (r.body as any).matches.length > 0) {
    const top = (r.body as any).matches[0]
    const relationAction = (type: string, issueKey: string) => runApply({
      requestId: r.requestId,
      action: r.action,
      subAction: r.subAction,
      field: r.field,
      fieldLabel: r.fieldLabel,
      issueId: r.issueId,
      body: r.body,
      intent: 'link-relation',
      values: { issue_key: issueKey, relation_type: type },
    })
    return {
      copy: t('ai.linkAsRelated', { issueKey: top.issue_key }),
      primary: {
        label: t('ai.linkRelated'),
        action: () => relationAction('related', top.issue_key),
      },
      secondary: [
        { label: t('ai.linkBlocks'), action: () => relationAction('blocks', top.issue_key) },
        { label: t('ai.linkDependsOn'), action: () => relationAction('depends_on', top.issue_key) },
      ],
      explain: {
        label: t('ai.moreRelations'),
        action: () => undefined,
      },
    }
  }
  return null
})

function clearError() {
  aiAction.clearError()
  aiOptimize.clearError()
}

async function runApply(args: ActionApplyArgs) {
  if (!props.apply) return
  try {
    const res = await props.apply(args)
    aiAction.clearError()
    modalOpen.value = false
    if (undoTimer) {
      window.clearTimeout(undoTimer)
      undoTimer = null
    }
    if (res?.undo) {
      undoState.value = res
      undoDismissMs.value = res.undoAutoDismissMs ?? 5000
      if (undoDismissMs.value > 0) {
        undoTimer = window.setTimeout(() => {
          undoState.value = null
          undoTimer = null
        }, undoDismissMs.value)
      }
    } else {
      undoState.value = null
    }
  } catch (e: any) {
    aiAction.lastError.value = e?.message ?? 'Apply failed'
    aiAction.lastErrorHostKey.value = props.hostKey
  }
}

async function undoLastApply() {
  if (!undoState.value?.undo) return
  await undoState.value.undo()
  undoState.value = null
  if (undoTimer) {
    window.clearTimeout(undoTimer)
    undoTimer = null
  }
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
      :details-mode="modalShapeAction ? 'modal' : 'inline'"
      :primary="actionDecision?.primary"
      :secondary="actionDecision?.secondary"
      :explain="actionDecision?.explain"
      :dismissable="true"
      :auto-dismiss-ms="actionDecision ? undefined : 12000"
      @details="modalOpen = true"
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
        <template v-if="actionResult.action === 'find_parent'">
          <div class="ai-inline-list">
            <div v-for="candidate in (actionResult.body as any)?.candidates ?? []" :key="candidate.issue_key" class="ai-inline-card">
              <strong>{{ candidate.issue_key }} — {{ candidate.title }}</strong>
              <span class="ai-inline-card__meta">{{ candidate.confidence || 'candidate' }}</span>
              <p>{{ candidate.rationale }}</p>
            </div>
          </div>
        </template>
        <template v-else-if="actionResult.action === 'estimate_effort'">
          <p>{{ (actionResult.body as any)?.reasoning || t('ai.detailsHint') }}</p>
        </template>
        <template v-else-if="actionResult.action === 'detect_duplicates'">
          <div class="ai-inline-list">
            <div v-for="match in (actionResult.body as any)?.matches ?? []" :key="match.issue_key" class="ai-inline-card">
              <strong>{{ match.issue_key }} — {{ match.title }}</strong>
              <span class="ai-inline-card__meta">{{ match.similarity || 'match' }}</span>
              <p>{{ match.rationale }}</p>
            </div>
          </div>
        </template>
        <template v-else-if="actionResult.action === 'tone_check'">
          <div class="ai-inline-list">
            <div class="ai-inline-card">
              <span class="ai-inline-card__meta">Current</span>
              <p>{{ actionResult.sourceText }}</p>
            </div>
            <div class="ai-inline-card">
              <span class="ai-inline-card__meta">Neutralized</span>
              <p>{{ (actionResult.body as any)?.optimized || (actionResult.body as any)?.optimized_text || '' }}</p>
            </div>
          </div>
        </template>
        <template v-else>
          <p>{{ t('ai.detailsHint') }}</p>
        </template>
      </div>
    </AiResultStrip>

    <AiActionResultModal :host-key="hostKey" :apply="apply" :manual="modalShapeAction" :open="modalOpen" />

    <AiResultStrip
      v-if="undoState?.undo"
      action-key="undo"
      :title="t('ai.undoTitle')"
      :summary="undoState.undoLabel || t('ai.undoReady')"
      :primary="{ label: t('ai.undo'), action: undoLastApply }"
      :dismissable="true"
      :auto-dismiss-ms="undoDismissMs"
      @dismiss="undoState = null"
    />

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
.ai-inline-list {
  display: flex;
  flex-direction: column;
  gap: .45rem;
}
.ai-inline-card {
  padding: .55rem .65rem;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg);
}
.ai-inline-card strong {
  display: block;
  font-size: 12px;
  color: var(--text);
}
.ai-inline-card__meta {
  display: inline-block;
  margin-top: .15rem;
  margin-bottom: .2rem;
  font-family: "DM Mono", "JetBrains Mono", monospace;
  font-size: 10px;
  color: var(--text-muted);
}
</style>
