import type { MaybeRefOrGetter } from "vue";

import { getDisplayLocale, useDisplayLocale } from "@/utils/displayLocale";

type DateInput = Date | string | number | null | undefined;

let displayTimezone: string | undefined;

export function setDisplayTimezone(tz: string | undefined) {
  displayTimezone = !tz || tz === "auto" ? undefined : tz;
}

export function ensureUTC(value: string): string {
  if (!value) return value;
  if (value.endsWith("Z") || /[+-]\d{2}:\d{2}$/.test(value)) return value;
  if (/^\d{4}-\d{2}-\d{2}$/.test(value)) return `${value}T00:00:00Z`;
  return value.replace(" ", "T") + "Z";
}

function toDate(value: DateInput): Date | null {
  if (value == null || value === "") return null;
  const date = value instanceof Date
    ? value
    : typeof value === "string"
      ? new Date(ensureUTC(value))
      : new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

function withTimezone(options: Intl.DateTimeFormatOptions): Intl.DateTimeFormatOptions {
  return displayTimezone ? { timeZone: displayTimezone, ...options } : options;
}

export function formatDateWithLocale(
  value: DateInput,
  locale?: string | null,
  options?: Intl.DateTimeFormatOptions,
): string {
  const date = toDate(value);
  if (!date) return "—";
  return new Intl.DateTimeFormat(getDisplayLocale(locale), withTimezone(options ?? {
    day: "numeric",
    month: "short",
    year: "numeric",
  })).format(date);
}

export function formatTimeWithLocale(
  value: DateInput,
  locale?: string | null,
  options?: Intl.DateTimeFormatOptions,
): string {
  const date = toDate(value);
  if (!date) return "—";
  return new Intl.DateTimeFormat(getDisplayLocale(locale), withTimezone(options ?? {
    hour: "2-digit",
    minute: "2-digit",
  })).format(date);
}

export function formatDateTimeWithLocale(
  value: DateInput,
  locale?: string | null,
  options?: Intl.DateTimeFormatOptions,
): string {
  const date = toDate(value);
  if (!date) return "—";
  return new Intl.DateTimeFormat(getDisplayLocale(locale), withTimezone(options ?? {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  })).format(date);
}

export function formatShortDateTimeWithLocale(
  value: DateInput,
  locale?: string | null,
): string {
  return formatDateTimeWithLocale(value, locale, {
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function formatRelativeTimeWithLocale(
  value: DateInput,
  locale?: string | null,
  nowMs = Date.now(),
): string {
  const date = toDate(value);
  if (!date) return "—";
  const diffSeconds = Math.round((date.getTime() - nowMs) / 1000);
  const absSeconds = Math.abs(diffSeconds);
  const rtf = new Intl.RelativeTimeFormat(getDisplayLocale(locale), { numeric: "auto" });
  if (absSeconds < 45) return rtf.format(0, "second");
  if (absSeconds < 45 * 60) return rtf.format(Math.round(diffSeconds / 60), "minute");
  if (absSeconds < 22 * 3600) return rtf.format(Math.round(diffSeconds / 3600), "hour");
  if (absSeconds < 7 * 86400) return rtf.format(Math.round(diffSeconds / 86400), "day");
  return formatDateWithLocale(date, locale, { day: "numeric", month: "short" });
}

export function useDateFormat(locale?: MaybeRefOrGetter<string | null | undefined>) {
  const resolvedLocale = useDisplayLocale(locale);
  return {
    locale: resolvedLocale,
    date: (value: DateInput, options?: Intl.DateTimeFormatOptions) =>
      formatDateWithLocale(value, resolvedLocale.value, options),
    time: (value: DateInput, options?: Intl.DateTimeFormatOptions) =>
      formatTimeWithLocale(value, resolvedLocale.value, options),
    dateTime: (value: DateInput, options?: Intl.DateTimeFormatOptions) =>
      formatDateTimeWithLocale(value, resolvedLocale.value, options),
    shortDateTime: (value: DateInput) =>
      formatShortDateTimeWithLocale(value, resolvedLocale.value),
    relative: (value: DateInput, nowMs = Date.now()) =>
      formatRelativeTimeWithLocale(value, resolvedLocale.value, nowMs),
  };
}
