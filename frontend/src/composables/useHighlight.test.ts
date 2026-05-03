import { describe, expect, it } from "vitest";
import { highlight } from "@/composables/useHighlight";

describe("highlight", () => {
  it("marks plain issue keys and titles", () => {
    expect(highlight("PAI-303", "PAI-303")).toBe(
      '<mark class="search-highlight">PAI-303</mark>',
    );
    expect(highlight("Submitted global search", "global")).toContain(
      '<mark class="search-highlight">global</mark>',
    );
  });

  it("marks regex-like search text without corrupting escaped HTML", () => {
    expect(highlight("A+B release", "A+B")).toContain(
      '<mark class="search-highlight">A+B</mark>',
    );
    expect(highlight("<b>literal</b>", "<b>literal</b>")).toContain(
      '<mark class="search-highlight">&lt;b&gt;literal&lt;/b&gt;</mark>',
    );
  });
});
