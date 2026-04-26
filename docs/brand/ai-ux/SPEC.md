# AI UX Spec

`PAI-201` design reference for the v2 AI assist layer.

## Intent

The AI layer should read like an editorial compile strip:

- no chat bubbles
- no celebratory motion
- visible telemetry
- compact inline decisions

## States

### Activity

- `idle`: no extra chrome
- `pending`: no strip before `250 ms`
- `working`: strip shows action title, phase, elapsed time, action key
- `stalled`: same strip, plus slower-provider note
- `failed`: muted red strip with direct error line
- `cancelled`: same visual family as failed

### Result

- result strips sit directly under the initiating control or field
- summary copy must be countable and action-specific
- result rows may auto-dismiss after `12 s` if no decision is required
- detail-heavy payloads still use the existing modal path

### Decision

- primary action first
- secondary actions stay ghosted
- keyboard intent:
  - `Enter` = primary
  - `Esc` = dismiss

## Typography

- display: `Bricolage Grotesque`
- body: `DM Sans`
- chrome and telemetry: `DM Mono` or `JetBrains Mono`

## Color

- active/info: existing `--bp-blue*`
- muted chrome: `--text-muted`
- failure: current app red family only

## Motion

- easing: `cubic-bezier(.2, .7, .1, 1)`
- duration: `<= 250 ms`
- reduced-motion: instant state changes

## A11y

- activity strips use `role="status"` and `aria-live="polite"`
- decorative sweep bars stay `aria-hidden`
- decision controls stay keyboard reachable without pointer-only affordances

## Flow

```text
idle
  -> pending
  -> working
    -> stalled
    -> failed
    -> result
      -> decision
        -> applied
        -> dismissed
```
