# PAIMOS brand assets

Three SVG files, all identical geometry — only the viewBox differs.
`mark.svg` is the canonical mark; the other two are frame variants for
specific contexts.

| File | viewBox | Purpose |
|---|---|---|
| `mark.svg` | `0 0 55 60` (tight) | README header, docs, in-app branding, GitHub avatar (auto-cropped to square) |
| `favicon.svg` | `-2.5 0 60 60` (60×60 square, mark centered) | Browser favicon |
| `mark-app.svg` | `-12.5 -10 80 80` (80×80 with ~15% padding) | PWA manifest icon, Apple touch icon, square app icon uses |

All three use `fill="currentColor"` — no hardcoded palette. They render
in whatever color the enclosing context sets. Light/dark mode just
works.

If the mark ever needs a geometry edit: change `mark.svg`, then copy
the four elements (`<rect>` × 3 + `<path>` × 1) into the other two
files. No other derivation logic.

See [`BRAND.md`](BRAND.md) for the full brand guide (name, voice,
visual spec, legal status).
