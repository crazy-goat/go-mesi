# [cli] Add `-includeErrorMarker` flag

## Problem

`EsiParserConfig.IncludeErrorMarker` is ⚠️ — settable programmatically but no CLI flag. Failed includes produce invisible output.

## Proposed solution

### Flag

```go
var includeErrorMarker = flag.String("includeErrorMarker", "",
    "Marker string rendered for failed ESI includes (e.g. '<!-- esi error -->')")
```

### Map

```go
config.IncludeErrorMarker = *includeErrorMarker
```

### Usage

```bash
mesi-cli -includeErrorMarker "<!-- esi error -->" input.html
```

## Acceptance criteria

- [ ] **Tests** — Unit test: string, empty, default ""
- [ ] **Docs** — Add to README with security warning
- [ ] **Functional tests** — Marker renders for failed include, not for onerror/fallback
- [ ] **Changelog** — Entry
