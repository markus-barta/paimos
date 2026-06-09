import { computed, toValue, type MaybeRefOrGetter } from "vue";

import i18n from "@/i18n";

let sessionLocale: string | undefined;

function cleanLocale(locale: unknown): string | undefined {
  if (typeof locale !== "string") return undefined;
  const trimmed = locale.trim();
  return trimmed || undefined;
}

function browserLocale(): string | undefined {
  if (typeof navigator === "undefined") return undefined;
  return cleanLocale(navigator.language);
}

export function setDisplayLocale(locale: string | null | undefined) {
  sessionLocale = cleanLocale(locale);
}

export function getDisplayLocale(explicit?: string | null): string {
  return (
    cleanLocale(explicit) ??
    sessionLocale ??
    cleanLocale(i18n.global.locale.value) ??
    browserLocale() ??
    "en-US"
  );
}

export function useDisplayLocale(locale?: MaybeRefOrGetter<string | null | undefined>) {
  return computed(() => getDisplayLocale(toValue(locale)));
}
