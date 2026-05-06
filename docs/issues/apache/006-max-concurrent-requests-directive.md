# [apache] Add `MesiMaxConcurrentRequests` directive

## Problem

`EsiParserConfig.MaxConcurrentRequests` (0 = unlimited) limits concurrent HTTP fetch goroutines within one `MESIParse` call. Apache calls `MESIParse` synchronously from the output filter. A page with 100 `<esi:include>` tags spawns 100 goroutines making concurrent HTTP requests.

This can exhaust file descriptors, overwhelm upstream backends, and cause memory pressure per Apache worker thread.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    int max_concurrent_requests;  // -1 = unset (0 = unlimited)
} mesi_config;
```

### 2. Add directive

```c
static const char *set_max_concurrent(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    int val = atoi(arg);
    if (val < 0) return "MesiMaxConcurrentRequests must be a non-negative integer";
    conf->max_concurrent_requests = val;
    return NULL;
}
```

Directive:
```c
AP_INIT_TAKE1("MesiMaxConcurrentRequests", set_max_concurrent, NULL, RSRC_CONF,
    "Max concurrent ESI HTTP requests per page render (0=unlimited, default: 0)"),
```

### 3. Default and merge

```c
conf->max_concurrent_requests = -1;
// Merge → -1 means 0 (unlimited, backward compatible)
```

### Apache config

```apache
MesiMaxConcurrentRequests 5
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `5`, `0`, `-1` (error)
- [ ] **Docs** — Add directive to README, clarify per-page-render scope
- [ ] **Functional tests** — Apache integration test:
  - Page with 20 includes to slow backend, `MesiMaxConcurrentRequests 3` → funneled through 3 concurrent slots
  - `MesiMaxConcurrentRequests 0` → unlimited (backward compat)
- [ ] **Changelog** — Entry

## Notes

- Scope: per-MESIParse call (one page render), not per-Apache-worker. Multiple concurrent requests each get their own goroutine pool.
- Requires `ParseJson` to pass the field — `ParseWithConfig` doesn't accept it.
- For Apache MPM worker/event, each thread independently calls `MESIParse`. The limit applies per-thread.
