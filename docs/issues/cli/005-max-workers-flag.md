# [cli] Add `-maxWorkers` flag

## Problem

`EsiParserConfig.MaxWorkers` not configurable from CLI.

## Proposed solution

### Flag

```go
var maxWorkers = flag.Int("maxWorkers", 0,
    "Max token-processing goroutines (0=runtime.NumCPU()*4)")
```

### Usage

```bash
mesi-cli -maxWorkers 8 input.html
```

## Acceptance criteria

- [ ] **Tests** — Unit test: valid, 0 → library default, negative → default
- [ ] **Docs** — Add to README
- [ ] **Changelog** — Entry
