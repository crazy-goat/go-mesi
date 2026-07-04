# mESI – Feature Matrix

Support status of mESI features across all server integrations.

| Feature | mESI Core | Traefik | RoadRunner | Caddy / FrankenPHP | Nginx | Apache | PHP Extension | CLI | Proxy |
|---|---|---|---|---|---|---|---|---|---|
| `<esi:include>` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<esi:remove>` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<esi:comment>` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<!--esi ... -->` (inline) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<esi:inline>` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<esi:choose>`, `<esi:when>`, `<esi:otherwise>` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<esi:try>`, `<esi:attempt>`, `<esi:except>` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<esi:vars>` / `$(...)` variable substitution | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `src` / `alt` attributes | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `fetch-mode="fallback"` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `fetch-mode="ab"` (A/B testing) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `ab-ratio` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `fetch-mode="concurrent"` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `timeout` (per-tag) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `max-depth` (per-tag) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `onerror="continue"` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Fallback content (tag body) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `IncludeErrorMarker` | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ⚠️ | ✅ |
| Global MaxDepth | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ |
| Global Timeout | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| SSRF (BlockPrivateIPs) | ✅ | 🔒 | 🔒 | ✅ | ❌ | ✅ | ❌ | 🔒 | ✅ |
| AllowedHosts | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ | ✅ |
| AllowPrivateIPsForAllowedHosts | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| MaxResponseSize | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| MaxConcurrentRequests | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| MaxWorkers | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| ParseOnHeader | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| Debug mode | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| Cache (in-memory) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ |
| Cache (Redis) | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ |
| Cache (Memcached) | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ |
| Custom CacheKeyFunc | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| Recursive ESI processing | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Shared HTTPClient | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ✅ |

## Legend

| Symbol | Meaning |
|---|---|
| ✅ | Supported and configurable |
| 🔒 | Always on (hardcoded, not configurable) |
| ⚠️ | Partial support / limitations |
| ❌ | Not supported / unavailable |

## Notes

- **`<esi:inline>`** – Fully supported. Body content is output verbatim without further ESI processing, serving as an escape hatch for literal ESI markup.
- **`<esi:choose>`, `<esi:when>`, `<esi:otherwise>`** – Fully supported. Boolean `test` attributes (`true`/`false`/`0`/`1`) select the first matching `<esi:when>` branch. If no branch matches, `<esi:otherwise>` is rendered (if present). `$(...)` variables in `test` are resolved before evaluation. Supports nested `<esi:choose>`, `<esi:include>`, `<esi:try>`, and `<esi:vars>` inside branch bodies.
- **`<esi:try>`, `<esi:attempt>`, `<esi:except>`** – Fully supported. Unhandled include errors within `<esi:attempt>` trigger `<esi:except>` rendering. `onerror="continue"` and fallback body do NOT trigger `<esi:except>`. Supports nested `<esi:try>` blocks.
- **`<esi:vars>` / `$(...)` variable substitution** – Fully supported. Variables are defined via `<esi:variable>` in `<esi:vars>` blocks and resolved via `$(NAME)` syntax in include URLs, text content, and test expressions. Supports `$(HTTP_HEADER{Name})`, `$(HTTP_COOKIE{name})`, and `$(QUERY_STRING{param})` via config fields.
- **Nginx** – Uses the `Parse` function from `libgomesi`. Supports `mesi_shared_http_client` for connection pooling. Shared client has SSRF protection (private IPs blocked by default).
- **PHP Extension** – Exposes only `\mesi\parse(input, max_depth, default_url)`. No security configuration.
- **Caddy / FrankenPHP** – FrankenPHP uses the Caddy plugin, identical functionality.
- **Proxy** – Accepts full `EsiParserConfig`; all features available when provided by calling code.
- **IncludeErrorMarker (CLI)** – Can only be set programmatically (no CLI flag).
- **`fetch-mode="ab"` (`ab-ratio`)** – Format requirements enforced since #315:
  - Exactly one `:` separator (`A:B`); leading and trailing whitespace is trimmed.
  - Each side must be a non-negative unsigned integer.
  - Either side may be zero; both zero (`0:0`) is rejected because no traffic would reach `Alt`.
  - Each side must be ≤ `MaxABRatio` (1,000,000) — protects downstream arithmetic from silent overflow.
  - Malformed values (missing colon, extra colons, non-numeric, negative, decimal, oversized, both-zero) yield `*ErrInvalidABRatio`. The error surfaces through the existing include-error path: rendered as `IncludeErrorMarker` (empty by default), skipped if `onerror="continue"`, or replaced with the tag body if provided. Empty `ab-ratio` falls back to the documented default `50:50`.
