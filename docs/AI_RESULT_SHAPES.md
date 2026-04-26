# AI Result Shapes

`PAI-204` reference for the frontend result strip and deep-detail viewers.

## Envelope

All action responses continue to use the existing action envelope:

```json
{
  "action": "estimate_effort",
  "sub_action": "",
  "model": "anthropic/claude-sonnet-4.5",
  "request_id": "01968f5f-....",
  "result": {}
}
```

The `result` payload is action-specific. Optional counters live under
`result.counters`.

## Actions

### `optimize` / `optimize_customer`

```json
{
  "optimized": "..."
}
```

Frontend summary: char / sentence delta from `source_text` vs. `optimized`.

### `translate`

```json
{
  "optimized": "..."
}
```

Frontend summary: translated copy length and sentence delta.

### `tone_check`

```json
{
  "optimized": "...",
  "counters": {
    "phrases_removed": 4
  }
}
```

Frontend details: current text vs. neutralized text inline, plus a replace-text apply path.

### `suggest_enhancement`

```json
{
  "suggestions": [
    {
      "title": "...",
      "body": "...",
      "impact": "high",
      "target_field": "ac"
    }
  ],
  "counters": {
    "items": 4,
    "categories": 2
  }
}
```

### `spec_out`

```json
{
  "items": [
    { "category": "behavior", "text": "..." }
  ],
  "counters": {
    "items": 6
  }
}
```

### `find_parent`

```json
{
  "candidates": [
    {
      "issue_key": "PAI-83",
      "title": "...",
      "score": 0.87,
      "confidence": "high",
      "rationale": "..."
    }
  ]
}
```

### `generate_subtasks`

```json
{
  "suggestions": [
    { "title": "...", "description": "..." }
  ],
  "counters": {
    "items": 5
  }
}
```

### `estimate_effort`

```json
{
  "hours": 6,
  "lp": 1,
  "confidence": "medium",
  "reasoning": "...",
  "counters": {
    "hours": 6,
    "lp": 1
  }
}
```

### `detect_duplicates`

```json
{
  "matches": [
    {
      "issue_key": "PAI-19",
      "title": "...",
      "score": 0.82
    }
  ],
  "counters": {
    "matches": 3
  }
}
```

### `ui_generation`

```json
{
  "spec_markdown": "...",
  "counters": {
    "words": 142
  }
}
```
