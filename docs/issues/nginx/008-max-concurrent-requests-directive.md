# [nginx] Add `max_concurrent_requests` directive

## Problem

`mesi.EsiParserConfig.MaxConcurrentRequests` (0 = unlimited) controls how many `<esi:include>` fetch goroutines can run simultaneously within a single `MESIParse` call.

```go
MaxConcurrentRequests int  // 0 = unlimited (backward compatible)
```

nginx calls `MESIParse` (via libgomesi) synchronously from the body filter — one call per request. Without a limit, a page with 100 ESI includes spawns 100 goroutines, all making HTTP requests concurrently. Under load:

- **File descriptor exhaustion** — nginx worker + Go runtime FDs compete.
- **Upstream saturation** — Backend receives 100 simultaneous requests from one page render.
- **Memory pressure** — Each goroutine buffers its response body.

## Impact

- nginx workers can spike in resource usage during ESI-heavy page renders.
- Backend upstreams experience thundering-herd from a single page with many includes.
- No backpressure mechanism — nginx cannot protect its backend services.

## Context

The `MaxConcurrentRequests` limit is enforced by a semaphore inside `mesi.EsiParserConfig` (line 48: `requestSemaphore`). When set > 0, goroutines acquire the semaphore before making HTTP requests and release it after. This limits concurrent outbound HTTP calls within one `MESIParse` invocation.

This works in CGo context: Go goroutines are transparent to the C caller. nginx calls `EsiParseJson()`, Go spawns goroutines limited by the semaphore, returns the result as a C string.

The feature is NOT nginx-worker-level — it limits concurrency within ONE page render (one `MESIParse` call), not across concurrent nginx requests.

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    // ... existing fields ...
    ngx_int_t   max_concurrent_requests;  // 0 = unlimited
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

```c
{ngx_string("mesi_max_concurrent_requests"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_num_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, max_concurrent_requests), NULL},
```

### 3. Default and merge

```c
ngx_conf_merge_value(conf->max_concurrent_requests, prev->max_concurrent_requests, 0);
// Default: 0 (unlimited, backward compatible)
```

### 4. Pass to libgomesi

```json
{"maxConcurrentRequests": 5}
```

Via `ParseJson` or a new `ParseWithConfigExtended` parameter.

### nginx.conf example

```nginx
location / {
    enable_mesi on;
    mesi_max_concurrent_requests 5;
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive: `5`, `0` (unlimited), `-1` (invalid)
- [ ] **Tests** — Unit test field propagation to `ParseJson` / config struct
- [ ] **Docs** — Add directive to README, clarifying "per-page-render" scope (not nginx-worker-global)
- [ ] **Functional tests** — nginx integration test:
  - Page with 20 includes to a slow backend (sleep 2s per response), `max_concurrent_requests 3`:
    - Verify: total response time > 3*2s (funneled through 3 concurrent slots) but < 20*2s (serialized)
    - Verify: backend sees at most 3 concurrent connections from the single page render
  - `max_concurrent_requests 0` (unlimited) → all includes fire concurrently
  - Stress test: `ab -n 100 -c 10` with page containing many includes, verify no file descriptor exhaustion
- [ ] **Changelog** — Entry in project changelog

## Notes

- **Scope**: This limits concurrency within ONE `MESIParse` call, not across multiple nginx worker threads. Each nginx worker calling `MESIParse` gets its own concurrency pool. If nginx has 4 workers each processing a request with `max_concurrent_requests 5`, the total concurrent outbound ESI connections could be up to 20 (4 workers × 5).
- The semaphore is created once per `EsiParserConfig`. When calling from CGo via `ParseJson`, the Go side creates the config, computes the semaphore, and uses it for that call. The semaphore is NOT shared across multiple nginx requests — each `Parse` call gets its own config and semaphore.
- For true nginx-global concurrency control, a separate mechanism would be needed (e.g., a shared semaphore in libgomesi init). This is beyond the scope of this issue.
- `ngx_conf_set_num_slot` works for `ngx_int_t` fields. Values must be non-negative. Validation for negative values can be added in a custom setter if needed beyond nginx's built-in range check.
