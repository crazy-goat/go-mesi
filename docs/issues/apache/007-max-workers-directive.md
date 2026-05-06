# [apache] Add `MesiMaxWorkers` directive

## Problem

`EsiParserConfig.MaxWorkers` caps goroutines processing ESI token tree nodes within one `MESIParse` call:

```go
MaxWorkers int  // 0 = runtime.NumCPU()*4
```

On large servers, the default can be hundreds of goroutines. Apache provides no way to cap this.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    int max_workers;  // -1 = unset (0 = library default)
} mesi_config;
```

### 2. Add directive

```c
static const char *set_max_workers(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    int val = atoi(arg);
    if (val < 0) return "MesiMaxWorkers must be a non-negative integer";
    conf->max_workers = val;
    return NULL;
}
```

Directive:
```c
AP_INIT_TAKE1("MesiMaxWorkers", set_max_workers, NULL, RSRC_CONF,
    "Max token-processing goroutines (0=NumCPU*4, default: 0)"),
```

### 3. Default and merge

```c
conf->max_workers = -1;  // → 0 (library default)
```

### Apache config

```apache
MesiMaxWorkers 8
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `4`, `0`, `-1` (error)
- [ ] **Docs** — Add directive to README, distinction from MaxConcurrentRequests
- [ ] **Functional tests** — Stress test with deep nesting, verify `MesiMaxWorkers 2` completes correctly
- [ ] **Changelog** — Entry

## Notes

- Token-processing is rarely the bottleneck (HTTP fetch dominates). This is for CPU-intensive include-tree traversal on deeply nested pages.
- `max_workers 1` serializes token processing — useful for debugging.
- Requires `ParseJson` to pass.
