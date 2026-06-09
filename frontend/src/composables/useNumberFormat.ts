import type { MaybeRefOrGetter } from "vue";

import { getDisplayLocale, useDisplayLocale } from "@/utils/displayLocale";

export function formatNumberWithLocale(
  value: number,
  locale?: string | null,
  options?: Intl.NumberFormatOptions,
): string {
  const resolvedLocale = getDisplayLocale(locale);
  const formatter = new Intl.NumberFormat(resolvedLocale, options);
  const parts = formatter.formatToParts(value);
  return parts
    .map((part) => {
      if (
        part.type === "group" &&
        resolvedLocale.toLowerCase().startsWith("de") &&
        /\s/u.test(part.value)
      ) {
        return ".";
      }
      return part.value;
    })
    .join("");
}

export function formatInteger(value: number | null | undefined, locale?: string | null): string {
  if (value == null || !Number.isFinite(Number(value))) return "—";
  return formatNumberWithLocale(Number(value), locale, { maximumFractionDigits: 0 });
}

export function formatDecimal(
  value: number | null | undefined,
  fractionDigits = 2,
  locale?: string | null,
): string {
  if (value == null || !Number.isFinite(Number(value))) return "—";
  return formatNumberWithLocale(Number(value), locale, {
    minimumFractionDigits: fractionDigits,
    maximumFractionDigits: fractionDigits,
  });
}

export function formatDecimalFlex(
  value: number | null | undefined,
  maximumFractionDigits = 2,
  locale?: string | null,
): string {
  if (value == null || !Number.isFinite(Number(value))) return "—";
  return formatNumberWithLocale(Number(value), locale, { maximumFractionDigits });
}

export function formatPercent(
  value: number | null | undefined,
  maximumFractionDigits = 1,
  locale?: string | null,
): string {
  if (value == null || !Number.isFinite(Number(value))) return "—";
  return formatNumberWithLocale(Number(value), locale, {
    style: "percent",
    maximumFractionDigits,
  });
}

export function formatFileSize(bytes: number | null | undefined, locale?: string | null): string {
  if (bytes == null || !Number.isFinite(Number(bytes))) return "—";
  const n = Math.max(0, Number(bytes));
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = n;
  let unit = units[0];
  for (let i = 0; i < units.length - 1 && value >= 1024; i += 1) {
    value /= 1024;
    unit = units[i + 1];
  }
  const formatted = unit === "B"
    ? formatInteger(value, locale)
    : formatDecimalFlex(value, 1, locale);
  return `${formatted} ${unit}`;
}

export function formatDurationHours(
  hours: number | null | undefined,
  locale?: string | null,
  maximumFractionDigits = 1,
): string {
  if (hours == null || !Number.isFinite(Number(hours))) return "—";
  return `${formatDecimalFlex(Number(hours), maximumFractionDigits, locale)}h`;
}

export function formatCurrency(
  value: number | null | undefined,
  currency = "EUR",
  locale?: string | null,
  options?: Intl.NumberFormatOptions,
): string {
  if (value == null || !Number.isFinite(Number(value))) return "—";
  return formatNumberWithLocale(Number(value), locale, {
    style: "currency",
    currency,
    ...options,
  });
}

export function formatCompactCurrency(
  value: number | null | undefined,
  currency = "EUR",
  locale?: string | null,
): string {
  if (value == null || !Number.isFinite(Number(value))) return "—";
  return formatNumberWithLocale(Number(value), locale, {
    style: "currency",
    currency,
    notation: "compact",
    maximumFractionDigits: 1,
  });
}

export function useNumberFormat(locale?: MaybeRefOrGetter<string | null | undefined>) {
  const resolvedLocale = useDisplayLocale(locale);

  function formatNumber(value: number, options?: Intl.NumberFormatOptions) {
    return formatNumberWithLocale(value, resolvedLocale.value, options);
  }

  return {
    locale: resolvedLocale,
    formatNumber,
    formatInteger: (value: number | null | undefined) => formatInteger(value, resolvedLocale.value),
    formatDecimal: (value: number | null | undefined, fractionDigits = 2) =>
      formatDecimal(value, fractionDigits, resolvedLocale.value),
    formatDecimalFlex: (value: number | null | undefined, maximumFractionDigits = 2) =>
      formatDecimalFlex(value, maximumFractionDigits, resolvedLocale.value),
    formatPercent: (value: number | null | undefined, maximumFractionDigits = 1) =>
      formatPercent(value, maximumFractionDigits, resolvedLocale.value),
    formatFileSize: (bytes: number | null | undefined) => formatFileSize(bytes, resolvedLocale.value),
    formatDurationHours: (
      hours: number | null | undefined,
      maximumFractionDigits = 1,
    ) => formatDurationHours(hours, resolvedLocale.value, maximumFractionDigits),
    formatCurrency: (
      value: number | null | undefined,
      currency = "EUR",
      options?: Intl.NumberFormatOptions,
    ) => formatCurrency(value, currency, resolvedLocale.value, options),
    formatCompactCurrency: (value: number | null | undefined, currency = "EUR") =>
      formatCompactCurrency(value, currency, resolvedLocale.value),
  };
}
