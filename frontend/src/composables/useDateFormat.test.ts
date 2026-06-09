import { describe, expect, it } from "vitest";

import {
  formatDateTimeWithLocale,
  formatDateWithLocale,
  formatRelativeTimeWithLocale,
  formatTimeWithLocale,
} from "./useDateFormat";

describe("useDateFormat", () => {
  it("formats dates and times for en-US, de-AT, and ja-JP", () => {
    const iso = "2026-06-09 10:30:00";

    expect(formatDateWithLocale(iso, "en-US")).toBe("Jun 9, 2026");
    expect(formatDateWithLocale(iso, "de-AT")).toBe("9. Juni 2026");
    expect(formatDateWithLocale(iso, "ja-JP")).toBe("2026年6月9日");

    expect(formatTimeWithLocale(iso, "en-US", {
      hour: "2-digit",
      minute: "2-digit",
      timeZone: "UTC",
    })).toMatch(/10:30\s?AM/);
    expect(formatDateTimeWithLocale(iso, "de-AT", {
      day: "numeric",
      month: "short",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      timeZone: "UTC",
    })).toContain("10:30");
  });

  it("formats relative labels through Intl.RelativeTimeFormat", () => {
    const now = Date.UTC(2026, 5, 9, 10, 30, 0);

    expect(formatRelativeTimeWithLocale(now - 2 * 60 * 1000, "en-US", now)).toBe("2 minutes ago");
    expect(formatRelativeTimeWithLocale(now - 2 * 60 * 1000, "de-AT", now)).toContain("2");
    expect(formatRelativeTimeWithLocale(now - 2 * 60 * 1000, "ja-JP", now)).toContain("2");
  });
});
