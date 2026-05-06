# [cli] Add `-maxConcurrentRequests` flag

## Problem

`EsiParserConfig.MaxConcurrentRequests` not configurable from CLI.

## Proposed solution

### Flag

```go
var maxConcurrentReqs = flag.Int("maxConcurrentRequests", 0,
    "Max concurrent ESI HTTP requests (0=unlimited)")
```

### Map

```go
config.MaxConcurrentRequests = *maxConcurrentReqs
```

### Usage

```bash
mesi-cli -maxConcurrentRequests 5 input.html
```

## Acceptance criteria

- [ ] **Tests** — Unit test: valid, 0 → unlimited, negative → default
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — 20 includes with limit 3 → funneled
- [ ] **Changelog** — Entry
