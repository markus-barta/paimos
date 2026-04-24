/*
 * PAIMOS — Your Professional & Personal AI Project OS
 * Copyright (C) 2026 Markus Barta <markus@barta.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public
 * License along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

/**
 * useTypeColors — singleton owner of issue-type color overrides.
 *
 * Owns the three `--type-{epic,ticket,task}` CSS custom properties.
 * Stores user overrides in localStorage; falls back to branding defaults
 * when an override is empty. `useBranding` calls `applyTypeColorsToDOM`
 * on first paint and on refresh; the watchers below keep things in sync
 * after user edits.
 */

import { ref, watch } from 'vue'
import { useBranding } from './useBranding'
import {
  LS_TYPE_COLOR_EPIC,
  LS_TYPE_COLOR_TICKET,
  LS_TYPE_COLOR_TASK,
} from '@/constants/storage'

interface TypeColorDefaults {
  typeEpic: string
  typeTicket: string
  typeTask: string
}

const epicOverride   = ref(localStorage.getItem(LS_TYPE_COLOR_EPIC)   || '')
const ticketOverride = ref(localStorage.getItem(LS_TYPE_COLOR_TICKET) || '')
const taskOverride   = ref(localStorage.getItem(LS_TYPE_COLOR_TASK)   || '')

function applyVar(name: 'epic' | 'ticket' | 'task', value: string) {
  document.documentElement.style.setProperty(`--type-${name}`, value)
}

function brandDefaults(): TypeColorDefaults {
  return useBranding().branding.value.colors
}

watch(epicOverride, v => {
  if (v) localStorage.setItem(LS_TYPE_COLOR_EPIC, v)
  else   localStorage.removeItem(LS_TYPE_COLOR_EPIC)
  applyVar('epic', v || brandDefaults().typeEpic)
})
watch(ticketOverride, v => {
  if (v) localStorage.setItem(LS_TYPE_COLOR_TICKET, v)
  else   localStorage.removeItem(LS_TYPE_COLOR_TICKET)
  applyVar('ticket', v || brandDefaults().typeTicket)
})
watch(taskOverride, v => {
  if (v) localStorage.setItem(LS_TYPE_COLOR_TASK, v)
  else   localStorage.removeItem(LS_TYPE_COLOR_TASK)
  applyVar('task', v || brandDefaults().typeTask)
})

/** Apply the current effective type colors to the DOM. Called by useBranding
 *  on first paint and on refresh so branding-default fallbacks reflect any
 *  branding change the watchers above wouldn't otherwise see. */
export function applyTypeColorsToDOM(brand: TypeColorDefaults) {
  applyVar('epic',   epicOverride.value   || brand.typeEpic)
  applyVar('ticket', ticketOverride.value || brand.typeTicket)
  applyVar('task',   taskOverride.value   || brand.typeTask)
}

export function useTypeColors() {
  return {
    epicOverride,
    ticketOverride,
    taskOverride,
  }
}

export function resetTypeColors() {
  epicOverride.value = ''
  ticketOverride.value = ''
  taskOverride.value = ''
}
