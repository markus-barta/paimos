import { describe, expect, it } from "vitest";

import { formatNumberWithLocale, useNumberFormat } from "./useNumberFormat";

describe("useNumberFormat", () => {
  it("formats integer counts using the provided locale", () => {
    expect(formatNumberWithLocale(21297, "en-US")).toBe("21,297");
    expect(formatNumberWithLocale(21297, "de-AT")).toBe("21.297");
  });

  it("uses a reactive fallback locale inside the composable", () => {
    const { formatNumber } = useNumberFormat("de-AT");
    expect(formatNumber(100000)).toBe("100.000");
  });
});
