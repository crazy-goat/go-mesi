# [cli] Add `-cacheKeyTemplate` flag

## Problem

`CacheKeyFunc` is Go function pointer — cannot be passed as CLI flag. Default `DefaultCacheKey` is URL-only.

## Proposed solution

Template-based flag:

### Flag

```go
var cacheKeyTemplate = flag.String("cacheKeyTemplate", "",
    "Cache key template: ${url}, ${header:Name}, ${cookie:Name}")
```

### Map (wraps template in closure)

```go
if *cacheKeyTemplate != "" {
    tmpl := *cacheKeyTemplate
    config.CacheKeyFunc = func(url string) string {
        result := tmpl
        result = strings.ReplaceAll(result, "${url}", url)
        // Note: CLI has no request headers/cookies. Template only supports ${url}.
        return result
    }
}
```

For URL mode (`-url`), the request headers from the fetched page could be made available, but this adds complexity. Initial implementation supports `${url}` only; `${header:X}` requires parsing headers from HTTP response in the CLI's fetch logic.

### Usage

```bash
mesi-cli -cacheKeyTemplate "mesi:${url}:v2" input.html
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `${url}` substitution, unknown placeholder → literal
- [ ] **Tests** — Unit test: absent → `DefaultCacheKey`
- [ ] **Docs** — Add to README, document limited placeholder support (CLI has no request context)
- [ ] **Changelog** — Entry

## Notes

- CLI is stateless — no request headers/cookies available. `${header:X}` and `${cookie:Y}` placeholders are not applicable unless `-url` mode extracts response headers. Document this limitation.
