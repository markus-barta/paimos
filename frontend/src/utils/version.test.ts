import { describe, expect, it } from "vitest";
import { formatDisplayVersion } from "@/utils/version";

describe("formatDisplayVersion", () => {
  it("keeps plain numeric versions", () => {
    expect(formatDisplayVersion("2.4.8")).toBe("2.4.8");
  });

  it("strips dev and build metadata from stamped images", () => {
    expect(formatDisplayVersion("2.4.8-dev+b0045c6")).toBe("2.4.8");
  });

  it("falls back to the trimmed value when no numeric version exists", () => {
    expect(formatDisplayVersion(" dev ")).toBe("dev");
  });
});
