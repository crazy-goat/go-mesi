# [cli] Add `-allowedHosts` flag

## Problem

`cli/mesi-cli.go` does not expose `AllowedHosts`. Every `<esi:include>` URL passes host validation.

## Proposed solution

### Flag

```go
var allowedHosts = flag.String("allowedHosts", "", "Comma-separated list of allowed hosts for ESI includes")
```

### Map in main()

```go
if *allowedHosts != "" {
    config.AllowedHosts = strings.Split(*allowedHosts, ",")
}
```

### Usage

```bash
mesi-cli -allowedHosts "backend.internal,cdn.example.com" input.html
```

## Acceptance criteria

- [ ] **Tests** — Unit test: flag parsed, comma → `[]string`, absent → nil
- [ ] **Tests** — Unit test: `-help` lists the flag with description
- [ ] **Docs** — Add to `cli/README.md`
- [ ] **Functional tests** — CLI test: include from allowed host → works, from unlisted → blocked
- [ ] **Changelog** — Entry
