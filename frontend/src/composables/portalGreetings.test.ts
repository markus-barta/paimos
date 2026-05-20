import { describe, expect, it } from "vitest";
import {
  _portalGreetingsForTest,
  portalGreeting,
} from "@/composables/portalGreetings";

describe("portalGreeting", () => {
  it("returns time-of-day prefix and resolved name", () => {
    const morning = portalGreeting("Maria", "mkatusic", "de", new Date(2026, 4, 20, 9, 0));
    expect(morning.prefix).toBe("Guten Morgen");
    expect(morning.name).toBe("Maria");

    const afternoon = portalGreeting(undefined, "mkatusic", "en", new Date(2026, 4, 20, 14, 0));
    expect(afternoon.prefix).toBe("Good afternoon");
    expect(afternoon.name).toBe("mkatusic");

    const evening = portalGreeting("", "", "de", new Date(2026, 4, 20, 20, 0));
    expect(evening.prefix).toBe("Guten Abend");
    expect(evening.name).toBe("dort");
  });

  it("picks a day-of-year stable message that changes by day", () => {
    const a = portalGreeting("X", "x", "en", new Date(2026, 4, 20, 10, 0));
    const sameDay = portalGreeting("X", "x", "en", new Date(2026, 4, 20, 22, 0));
    const nextDay = portalGreeting("X", "x", "en", new Date(2026, 4, 21, 10, 0));
    expect(a.message).toBe(sameDay.message);
    expect(a.message).not.toBe(nextDay.message);
  });

  it("ships at least 30 messages per locale, no exclamation marks or 'ship'/'let's'", () => {
    for (const locale of ["en", "de"] as const) {
      const list = _portalGreetingsForTest[locale];
      expect(list.length).toBeGreaterThanOrEqual(30);
      for (const msg of list) {
        expect(msg).not.toMatch(/!/);
        expect(msg.toLowerCase()).not.toMatch(/\blet'?s\b/);
        expect(msg.toLowerCase()).not.toMatch(/\bship\b/);
      }
    }
  });

  it("falls back to English for unknown locales", () => {
    const r = portalGreeting("A", "a", "fr" as never, new Date(2026, 4, 20, 10, 0));
    expect(r.prefix).toBe("Good morning");
  });
});
