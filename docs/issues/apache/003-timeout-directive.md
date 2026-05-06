# [apache] Add `MesiTimeout` directive

## Problem

`libgomesi/libgomesi.go:90` hardcodes `Timeout` to 30 seconds in `ParseWithConfig`:

```go
config := mesi.EsiParserConfig{
    // ...
    Timeout: 30 * time.Second,  // hardcoded
}
```

Apache calls this function at `mod_mesi.c:301`. The 30-second timeout cannot be tuned.

## Impact

- Operators with strict SLAs cannot enforce tighter timeouts.
- Backends with legitimate slow responses (>30s) time out unnecessarily.
- No visibility into timeout events.

## Context

`EsiParseWithConfig` (5-arg signature) does NOT accept a timeout parameter. To expose timeout, either:
- Extend the signature to 6 args (parameter creep)
- Switch to `EsiParseJson` (recommended for maintainability)

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    int timeout_seconds;  // -1 = unset (use default 30)
} mesi_config;
```

### 2. Add directive

```c
static const char *set_timeout(cmd_parms *cmd, void *cfg, const char *arg) {
    mesi_config *conf = (mesi_config *) ap_get_module_config(cmd->server->module_config, &mesi_module);
    int val = atoi(arg);
    if (val < 0) {
        return "MesiTimeout must be a non-negative integer (seconds)";
    }
    conf->timeout_seconds = val;
    return NULL;
}
```

Directive:
```c
AP_INIT_TAKE1("MesiTimeout", set_timeout, NULL, RSRC_CONF,
    "ESI processing timeout in seconds (default: 30, 0 = no timeout)"),
```

### 3. Default and merge

```c
conf->timeout_seconds = -1;  // unset → use 30

// Merge:
conf->timeout_seconds = (add->timeout_seconds != -1) ? add->timeout_seconds : base->timeout_seconds;
```

### 4. Pass to libgomesi

Via `ParseJson`:
```c
int timeout = (conf->timeout_seconds != -1) ? conf->timeout_seconds : 30;
snprintf(json, sizeof(json), "{\"timeout\":%llu}", (unsigned long long)timeout * 1000000000ULL);
```

### Apache config

```apache
EnableMesi On
MesiTimeout 10
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `MesiTimeout 10`, `MesiTimeout 0` (no timeout), `MesiTimeout -1` (error)
- [ ] **Tests** — Unit test default: unset → 30 seconds
- [ ] **Tests** — Unit test merge precedence
- [ ] **Docs** — Add directive to README, warn about 0 = unlimited
- [ ] **Functional tests** — Apache integration test:
  - `MesiTimeout 2` → backend include sleeps 5s → include fails within 2s
  - `MesiTimeout 30` → backend include responds at 10s → succeeds
  - Default (unset) → 30s timeout
- [ ] **Changelog** — Entry

## Notes

- Integer seconds is the simplest interface. Could accept "Xs" format later.
- 0 = no timeout means the include waits indefinitely. Warn about goroutine leaks if backends hang.
- Value passed to Go side as nanoseconds (int64): `int64(timeout_seconds) * 1e9`.
