import { describe, expect, it } from "vitest";
import {
  captureChord,
  matchesShortcut,
  parseShortcut,
  serializeShortcut,
} from "@/composables/searchScopeShortcut";

describe("searchScopeShortcut", () => {
  it("captures modifier chords and matches by physical key code", () => {
    const chord = captureChord(
      new KeyboardEvent("keydown", {
        key: "j",
        code: "KeyJ",
        ctrlKey: true,
      }),
    );

    expect(chord).toMatchObject({
      ctrl: true,
      shift: false,
      alt: false,
      meta: false,
      code: "KeyJ",
      key: "j",
      label: "Ctrl+J",
    });
    expect(
      matchesShortcut(
        new KeyboardEvent("keydown", {
          key: "j",
          code: "KeyJ",
          ctrlKey: true,
        }),
        chord,
      ),
    ).toBe(true);
    expect(
      matchesShortcut(
        new KeyboardEvent("keydown", {
          key: "j",
          code: "KeyJ",
          ctrlKey: true,
          shiftKey: true,
        }),
        chord,
      ),
    ).toBe(false);
    expect(
      matchesShortcut(
        new KeyboardEvent("keydown", {
          key: "j",
          code: "KeyK",
          ctrlKey: true,
        }),
        chord,
      ),
    ).toBe(false);
  });

  it("rejects no-modifier and shift-only captures", () => {
    expect(
      captureChord(
        new KeyboardEvent("keydown", {
          key: "j",
          code: "KeyJ",
        }),
      ),
    ).toBeNull();
    expect(
      captureChord(
        new KeyboardEvent("keydown", {
          key: "j",
          code: "KeyJ",
          shiftKey: true,
        }),
      ),
    ).toBeNull();
    expect(
      captureChord(
        new KeyboardEvent("keydown", {
          key: "Control",
          code: "ControlLeft",
          ctrlKey: true,
        }),
      ),
    ).toBeNull();
  });

  it("round-trips stored shortcuts and treats invalid storage as disabled", () => {
    const chord = {
      ctrl: true,
      shift: false,
      alt: false,
      meta: false,
      code: "KeyJ",
      key: "j",
      label: "Ctrl+J",
    };

    expect(parseShortcut(serializeShortcut(chord))).toEqual(chord);
    expect(parseShortcut("")).toBeNull();
    expect(parseShortcut("{")).toBeNull();
    expect(parseShortcut(JSON.stringify({ ctrl: true }))).toBeNull();
    expect(parseShortcut(JSON.stringify({ shift: true, code: "KeyJ" }))).toBeNull();
  });
});
