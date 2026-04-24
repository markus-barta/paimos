<!--
 PAIMOS — Your Professional & Personal AI Project OS
 Copyright (C) 2026 Markus Barta <markus@barta.com>

 This program is free software: you can redistribute it and/or modify
 it under the terms of the GNU Affero General Public License as
 published by the Free Software Foundation, version 3.
-->

<!--
 ProviderBadge — the one place "the customer is linked to an external CRM"
 is rendered. Every consumer (customer cards, detail header, integrations
 list) goes through here so a future second provider lights up without
 hunting for "HubSpot" strings.

 Two variants — `compact` for cards (icon-only with provider name in the
 tooltip), `full` for the detail header (icon + name + optional ↗).
-->
<script setup lang="ts">
import { computed } from 'vue'
import AppIcon from '@/components/AppIcon.vue'
import { useExternalProvider } from '@/composables/useExternalProvider'

const props = withDefaults(defineProps<{
  /** External provider id from the customer row, or null for manual customers. */
  providerId: string | null
  /** External URL — turns the badge into a link when present. */
  externalUrl?: string | null
  variant?: 'compact' | 'full'
}>(), {
  externalUrl: null,
  variant: 'compact',
})

const { provider } = useExternalProvider(props.providerId)

// Show even when the provider id is unknown to the registry — happens if
// a provider was once compiled in then removed. Falls back to a globe icon
// + the raw id so the badge still surfaces "this is externally linked".
const visible = computed(() => !!props.providerId)
const label = computed(() => provider.value?.name ?? props.providerId ?? '')
const logoUrl = computed(() => provider.value?.logo_url ?? '')
</script>

<template>
  <component
    :is="externalUrl ? 'a' : 'span'"
    v-if="visible"
    :href="externalUrl ?? undefined"
    :target="externalUrl ? '_blank' : undefined"
    :rel="externalUrl ? 'noopener noreferrer' : undefined"
    :class="['provider-badge', `provider-badge--${variant}`]"
    :title="`Linked to ${label}` + (externalUrl ? ' — open in CRM' : '')"
    @click.stop
  >
    <img v-if="logoUrl" :src="logoUrl" :alt="label" class="pb-logo" />
    <AppIcon v-else name="globe" :size="variant === 'full' ? 14 : 12" />
    <span v-if="variant === 'full'" class="pb-name">{{ label }}</span>
    <AppIcon v-if="externalUrl && variant === 'full'" name="external-link" :size="12" class="pb-arrow" />
  </component>
</template>

<style scoped>
.provider-badge {
  display: inline-flex;
  align-items: center;
  gap: .35rem;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: .02em;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-card);
  color: var(--text-muted);
  padding: .15rem .55rem .15rem .4rem;
  transition: border-color .15s, color .15s, background .15s;
  text-decoration: none;
  white-space: nowrap;
  font-family: 'DM Sans', system-ui, sans-serif;
}
.provider-badge:hover {
  border-color: var(--bp-blue);
  color: var(--bp-blue-dark);
  background: var(--bp-blue-pale);
}
.provider-badge--compact {
  width: 22px; height: 22px; padding: 0;
  justify-content: center;
}
.pb-logo {
  width: 14px; height: 14px;
  object-fit: contain;
  /* Match logos to the muted text color in idle state, snap to full
     on hover. Falls back to brand color for SVGs that don't ship a
     monochrome variant. */
  filter: grayscale(1) opacity(.65);
  transition: filter .15s;
}
.provider-badge:hover .pb-logo { filter: none; }
.pb-name { font-weight: 600; }
.pb-arrow { opacity: .55; }
.provider-badge:hover .pb-arrow { opacity: 1; }
</style>
