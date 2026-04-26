import { computed, type MaybeRefOrGetter, toValue } from "vue";

export function formatNumberWithLocale(
  value: number,
  locale?: string,
  options?: Intl.NumberFormatOptions,
): string {
  const formatter = new Intl.NumberFormat(locale, options);
  const parts = formatter.formatToParts(value);
  return parts
    .map((part) => {
      if (
        part.type === "group" &&
        locale?.toLowerCase().startsWith("de") &&
        /\s/u.test(part.value)
      ) {
        return ".";
      }
      return part.value;
    })
    .join("");
}

export function useNumberFormat(locale?: MaybeRefOrGetter<string | undefined>) {
  // Central helper for locale-aware numeric output. Callers can pass an
  // explicit locale for tests or future user-level preferences; otherwise
  // the browser locale wins.
  const resolvedLocale = computed(
    () => toValue(locale) ?? navigator.language ?? undefined,
  );

  function formatNumber(value: number, options?: Intl.NumberFormatOptions) {
    return formatNumberWithLocale(value, resolvedLocale.value, options);
  }

  return {
    locale: resolvedLocale,
    formatNumber,
  };
}
