# [cli] Add `-sharedHTTPClient` flag

## Problem

`EsiParserConfig.HTTPClient` is `nil` — each include creates fresh `http.Client`. No TCP connection reuse within a single CLI invocation.

## Proposed solution

### Flag

```go
var sharedHTTPClient = flag.Bool("sharedHTTPClient", false,
    "Share HTTP client across ESI includes for connection pooling")
```

### Map in main()

```go
if *sharedHTTPClient {
    config.HTTPClient = &http.Client{
        Transport: mesi.NewSSRFSafeTransport(config),
        Timeout:   config.Timeout,
    }
}
```

### Usage

```bash
mesi-cli -sharedHTTPClient -url https://example.com/page-with-many-includes.html
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `true` → shared transport created, `false`/absent → per-request clients
- [ ] **Docs** — Add to README
- [ ] **Functional tests** — Page with 10 includes to same origin → measurable latency improvement with flag
- [ ] **Changelog** — Entry
