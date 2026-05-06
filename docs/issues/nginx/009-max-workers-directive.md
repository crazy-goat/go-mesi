# [nginx] Add `max_workers` directive

## Problem

`mesi.EsiParserConfig.MaxWorkers` caps the number of goroutines processing ESI tokens within a single `MESIParse` call:

```go
MaxWorkers int  // 0 = runtime.NumCPU()*4, caps token-processing goroutines
```

On a 64-core machine, the default is 256 goroutines processing ESI token nodes. While goroutines are cheap, unbounded token processing goroutines create CPU contention under burst traffic. Operators have no way to tune this.

## Impact

- Large-CPU machines create excessive goroutines (256 on 64 cores) for token processing.
- Operators running nginx on shared infrastructure cannot limit Go-level CPU usage.
- No visibility into goroutine count or token-processing parallelism.

## Context

`MaxWorkers` limits goroutines processing **ESI token tree nodes**, not HTTP fetch goroutines (which are limited by `MaxConcurrentRequests`). These are distinct pools:

| Pool | Field | What it limits |
|---|---|---|
| Fetch goroutines | `MaxConcurrentRequests` | Concurrent HTTP calls for `<esi:include>` |
| Token-processing goroutines | `MaxWorkers` | Concurrent token tree traversal/rendering |

Both are per-`MESIParse` call, not nginx-worker-global.

## Proposed solution

### 1. Extend loc_conf_t

```c
typedef struct {
    // ... existing fields ...
    ngx_int_t   max_workers;  // 0 = runtime.NumCPU()*4 (default)
} ngx_http_mesi_loc_conf_t;
```

### 2. Add directive

```c
{ngx_string("mesi_max_workers"), NGX_HTTP_LOC_CONF | NGX_CONF_TAKE1,
 ngx_conf_set_num_slot, NGX_HTTP_LOC_CONF_OFFSET,
 offsetof(ngx_http_mesi_loc_conf_t, max_workers), NULL},
```

### 3. Default and merge

```c
ngx_conf_merge_value(conf->max_workers, prev->max_workers, 0);
// 0 = library default (runtime.NumCPU() * 4)
```

### 4. Pass to libgomesi

```json
{"maxWorkers": 4}
```

### nginx.conf example

```nginx
location / {
    enable_mesi on;
    mesi_max_workers 8;
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test directive: `4`, `1`, `0` (library default), `-1` (invalid)
- [ ] **Tests** — Unit test that 0 → omitted from JSON (or sent as 0 → library handles default)
- [ ] **Docs** — Add directive to README with distinction vs `max_concurrent_requests`
- [ ] **Functional tests** — Stress test: page with deeply nested include tree, `max_workers 2` vs `max_workers 100`, verify both complete correctly
- [ ] **Changelog** — Entry in project changelog

## Notes

- `MaxWorkers: 0` means "use library default" which is `runtime.NumCPU() * 4`. This is typically correct and sufficient.
- Token-processing goroutines are NOT the bottleneck for most ESI workloads. HTTP fetch latency dominates. `MaxWorkers` is most useful for pages with very deep/include-heavy markup where token tree traversal is CPU-intensive.
- Setting `max_workers 1` effectively serializes token processing (single goroutine). This is useful for debugging but not recommended for production.
- For `ParseJson`, if the field is 0, it can be omitted from JSON and the library's default applies. Or send 0 and ensure the library treats 0 as "use default" (confirm `mesi/config.go` behavior).
