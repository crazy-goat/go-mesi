# [caddy] Add `include_error_marker` Caddyfile directive

## Problem

`EsiParserConfig.IncludeErrorMarker` is always empty. Failed includes produce invisible output.

## Proposed solution

### 1. Add field

```go
type MesiMiddleware struct {
    // ...
    IncludeErrorMarker string `json:"include_error_marker,omitempty"`
}
```

### 2. Parse

```go
case "include_error_marker":
    if !d.NextArg() { return d.ArgErr() }
    m.IncludeErrorMarker = d.Val()
```

### 3. Map

```go
config.IncludeErrorMarker = m.IncludeErrorMarker
```

### Caddyfile

```
mesi {
    include_error_marker "<!-- esi error -->"
}
```

## Acceptance criteria

- [ ] **Tests** — Unit test: marker string with spaces/quotes, empty string
- [ ] **Docs** — Add directive to README with security warning (no internal details in marker)
- [ ] **Functional tests** — Integration test:
  - Marker set → failed include renders marker in HTML
  - `onerror="continue"` → marker NOT rendered
  - Fallback body → marker NOT rendered
- [ ] **Changelog** — Entry

## Notes

- Caddyfile values with spaces must be quoted.
- Security: marker is a static operator-configured string. Never include error messages or URLs.
- Marker renders only when no `onerror` and no fallback body on the tag.
