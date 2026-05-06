# [cli] Add `-maxResponseSize` flag

## Problem

`EsiParserConfig.MaxResponseSize` uses default 10 MB from `CreateDefaultConfig()`. No CLI flag to override.

## Proposed solution

### Flag

```go
var maxResponseSize = flag.Int64("maxResponseSize", 0,
    "Max ESI include response size in bytes (0=default 10MB)")
```

### Map

```go
if *maxResponseSize > 0 {
    config.MaxResponseSize = *maxResponseSize
}
```

### Usage

```bash
mesi-cli -maxResponseSize 1048576 input.html
```

## Acceptance criteria

- [ ] **Tests** — Unit test: valid, 0 → default, negative → ignore
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — 100 byte limit → 200 byte include rejected
- [ ] **Changelog** — Entry
