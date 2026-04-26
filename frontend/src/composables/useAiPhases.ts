import { computed, onBeforeUnmount, onMounted, ref, type Ref } from 'vue'
import i18n from '@/i18n'

export type AiPhase = 'pending' | 'working' | 'stalled' | 'failed' | 'cancelled'

export interface AiNarrationPhase {
  phase: string
  label: string
}

interface PhaseStep {
  at: number
  phase: string
}

const PENDING_MS = 250
const STALLED_MS = 8_000

const phaseScripts: Record<string, PhaseStep[]> = {
  optimize: [
    { at: 0, phase: 'reading' },
    { at: 1500, phase: 'composing' },
    { at: 3500, phase: 'refining' },
  ],
  optimize_customer: [
    { at: 0, phase: 'reading' },
    { at: 1500, phase: 'composing' },
    { at: 3500, phase: 'refining' },
  ],
  translate: [
    { at: 0, phase: 'reading' },
    { at: 1400, phase: 'translating' },
    { at: 3200, phase: 'polishing' },
  ],
  tone_check: [
    { at: 0, phase: 'reading' },
    { at: 1400, phase: 'screening' },
    { at: 3200, phase: 'softening' },
  ],
  suggest_enhancement: [
    { at: 0, phase: 'reading' },
    { at: 1600, phase: 'probing' },
    { at: 3400, phase: 'grouping' },
  ],
  spec_out: [
    { at: 0, phase: 'reading' },
    { at: 1600, phase: 'structuring' },
    { at: 3400, phase: 'tightening' },
  ],
  find_parent: [
    { at: 0, phase: 'reading' },
    { at: 1200, phase: 'scanning' },
    { at: 3000, phase: 'ranking' },
  ],
  generate_subtasks: [
    { at: 0, phase: 'reading' },
    { at: 1500, phase: 'sequencing' },
    { at: 3300, phase: 'sizing' },
  ],
  estimate_effort: [
    { at: 0, phase: 'reading' },
    { at: 1500, phase: 'comparing' },
    { at: 3500, phase: 'weighing' },
  ],
  detect_duplicates: [
    { at: 0, phase: 'reading' },
    { at: 1300, phase: 'matching' },
    { at: 3100, phase: 'ranking' },
  ],
  ui_generation: [
    { at: 0, phase: 'reading' },
    { at: 1600, phase: 'drafting' },
    { at: 3400, phase: 'formatting' },
  ],
}

function phaseLabelKey(actionKey: string, phase: string) {
  return `ai.phaseScript.${actionKey}.${phase}`
}

export function phaseFor(actionKey: string, elapsedMs: number): AiNarrationPhase {
  const script = phaseScripts[actionKey] ?? phaseScripts.optimize
  let current = script[0]
  for (const step of script) {
    if (elapsedMs >= step.at) current = step
  }
  const key = phaseLabelKey(actionKey, current.phase)
  return {
    phase: current.phase,
    label: i18n.global.te(key) ? String(i18n.global.t(key)) : String(i18n.global.t('ai.phase.working')),
  }
}

export function useAiPhases(actionKey: Ref<string>, startedAt: Ref<number | undefined>, failure = ref(false)) {
  const now = ref(Date.now())
  let timer: number | null = null

  function tick() {
    now.value = Date.now()
  }

  onMounted(() => {
    timer = window.setInterval(tick, 250)
  })
  onBeforeUnmount(() => {
    if (timer !== null) window.clearInterval(timer)
  })

  const elapsedMs = computed(() => {
    if (!startedAt.value) return 0
    return Math.max(0, now.value - startedAt.value)
  })

  const phase = computed<AiPhase>(() => {
    if (failure.value) return 'failed'
    if (!startedAt.value || elapsedMs.value < PENDING_MS) return 'pending'
    if (elapsedMs.value >= STALLED_MS) return 'stalled'
    return 'working'
  })

  const narration = computed(() => phaseFor(actionKey.value, elapsedMs.value))
  const phaseKey = computed(() => `ai.phase.${phase.value}`)

  return {
    elapsedMs,
    phase,
    phaseKey,
    narration,
  }
}
