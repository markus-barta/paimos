import { describe, expect, it } from "vitest";

import {
  formatCurrency,
  formatDecimal,
  formatDurationHours,
  formatFileSize,
  formatNumberWithLocale,
  formatPercent,
  useNumberFormat,
} from "./useNumberFormat";

describe("useNumberFormat", () => {
  it("formats integer counts using the provided locale", () => {
    expect(formatNumberWithLocale(21297, "en-US")).toBe("21,297");
    expect(formatNumberWithLocale(21297, "de-AT")).toBe("21.297");
  });

  it("uses a reactive fallback locale inside the composable", () => {
    const { formatNumber } = useNumberFormat("de-AT");
    expect(formatNumber(100000)).toBe("100.000");
  });

  it("formats display variants for en-US, de-AT, and ja-JP", () => {
    expect(formatDecimal(1234.5, 1, "en-US")).toBe("1,234.5");
    expect(formatDecimal(1234.5, 1, "de-AT")).toBe("1.234,5");
    expect(formatDecimal(1234.5, 1, "ja-JP")).toBe("1,234.5");

    expect(formatPercent(0.123, 1, "de-AT")).toBe("12,3\u00a0%");
    expect(formatFileSize(1536, "de-AT")).toBe("1,5 KB");
    expect(formatDurationHours(1.25, "de-AT")).toBe("1,3h");
    expect(formatCurrency(12.5, "EUR", "en-US")).toBe("€12.50");
  });
});
