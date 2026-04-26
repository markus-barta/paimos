import { ref } from 'vue'

export type ProjectAuxPanel = 'docs' | 'cooperation' | null

export function useProjectAuxPanels() {
  const auxPanel = ref<ProjectAuxPanel>(null)
  const docCount = ref(0)
  const cooperationPopulated = ref(false)

  function toggleAux(panel: Exclude<ProjectAuxPanel, null>) {
    auxPanel.value = auxPanel.value === panel ? null : panel
  }

  function closeAux() {
    auxPanel.value = null
  }

  return {
    auxPanel,
    toggleAux,
    closeAux,
    docCount,
    cooperationPopulated,
  }
}
