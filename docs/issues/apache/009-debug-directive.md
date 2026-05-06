# [apache] Add `MesiDebug` directive

## Problem

`EsiParserConfig.Debug` enables debug logging from the Go parser. When `false` (default), the parser is silent. Apache provides no way to enable debug output for troubleshooting ESI processing.

## Proposed solution

### 1. Extend mesi_config

```c
typedef struct {
    // ...
    int debug;  // -1=unset, 0=off, 1=on
} mesi_config;
```

### 2. Add directive

```c
AP_INIT_FLAG("MesiDebug", set_debug, NULL, RSRC_CONF,
    "Enable debug logging from ESI parser (default: Off)"),
```

### 3. Default and merge

```c
conf->debug = -1;  // → 0 (off)
// merge: conf->debug = (add->debug != -1) ? add->debug : base->debug;
```

### 4. Pass to libgomesi

```json
{"debug": true}
```

### Apache config

```apache
MesiDebug On
```

## Acceptance criteria

- [ ] **Tests** — Unit test: `On` / `Off`, default `Off`
- [ ] **Docs** — Add to README, note output goes to stderr (visible in Apache error log)
- [ ] **Functional tests** — Integration test:
  - `MesiDebug On` → debug messages appear in Apache error log
  - `MesiDebug Off` → no debug output
  - `MesiDebug On` + failed include → error details in debug output
- [ ] **Changelog** — Entry

## Notes

- Go's `log.Default()` writes to stderr. In Apache's child process, stderr may be redirected. Check where stderr goes for your Apache MPM.
- Debug output includes fetch URLs — potential info leak in shared logs. Warn in docs.
- Not for long-term production use due to stderr volume.
