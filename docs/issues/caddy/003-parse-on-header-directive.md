# [caddy] Add `parse_on_header` Caddyfile directive

## Problem

`servers/caddy/mesi.go` does not set `ParseOnHeader` — always `false`. Backends cannot signal ESI processing intent via `Surrogate-Control` response header.

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    ParseOnHeader bool `json:"parse_on_header,omitempty"`
}
```

### 2. Parse

```go
case "parse_on_header":
    m.ParseOnHeader = true
```

### 3. Check in ServeHTTP + pass to config

```go
config := mesi.EsiParserConfig{
    ParseOnHeader: m.ParseOnHeader,
    // ...
}

// In request handling: if ParseOnHeader, check header before buffering
if m.ParseOnHeader {
    sc := customWriter.Header().Get("Surrogate-Control")
    if sc == "" || !strings.Contains(sc, "ESI") {
        // passthrough without ESI parsing
        rw.Write(customWriter.Body().Bytes())
        return
    }
}
```

### Caddyfile

```
mesi {
    parse_on_header
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: directive present → true, absent → false
- [ ] **Docs** — Add directive to README
- [ ] **Functional tests** — Caddy integration test:
  - `parse_on_header` + backend returns no ESI header → ESI NOT processed
  - `parse_on_header` + backend returns `Surrogate-Control: content="ESI/1.0"` → ESI processed
  - Directive absent → ESI always processed for HTML (backward compat)
- [ ] **Changelog** — Entry

## Notes

- The C-side header check prevents unnecessary body buffering when ESI is not signaled.
- Case-insensitive substring match for "ESI" in `Surrogate-Control` value.
- Also check `Edge-Control` header for broader compatibility.
