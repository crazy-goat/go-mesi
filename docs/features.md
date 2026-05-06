# mESI – Feature Matrix

Support status of mESI features across all server integrations.

| Feature | mESI Core | Traefik | RoadRunner | Caddy / FrankenPHP | Nginx | Apache | PHP Extension | CLI | Proxy |
|---|---|---|---|---|---|---|---|---|---|
| `<esi:include>` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<esi:remove>` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<esi:comment>` | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<!--esi ... -->` (inline) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| `<esi:inline>`, `<esi:choose>`, `<esi:try>`, `<esi:vars>` | ⚠️ | ⚠️ | ⚠️ | ⚠️ | ⚠️ | ⚠️ | ⚠️ | ⚠️ | ⚠️ |
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
| SSRF (BlockPrivateIPs) | ✅ | 🔒 | 🔒 | 🔒 | ❌ | ✅ | ❌ | 🔒 | ✅ |
| AllowedHosts | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ | ✅ |
| AllowPrivateIPsForAllowedHosts | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| MaxResponseSize | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| MaxConcurrentRequests | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| MaxWorkers | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| ParseOnHeader | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| Debug mode | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| Cache (in-memory) | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| Cache (Redis) | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| Cache (Memcached) | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| Custom CacheKeyFunc | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| Recursive ESI processing | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Shared HTTPClient | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |

## Legend

| Symbol | Meaning |
|---|---|
| ✅ | Supported and configurable |
| 🔒 | Always on (hardcoded, not configurable) |
| ⚠️ | Partial support / limitations |
| ❌ | Not supported / unavailable |

## Notes

- **`<esi:inline>`, `<esi:choose>`, `<esi:try>`, `<esi:vars>`** – Recognized by the tokenizer (no parse errors), but their content is silently dropped from output. Full support planned.
- **Nginx** – Uses the `Parse` function from `libgomesi` which does not enable `BlockPrivateIPs` (defaults to `false`). No SSRF protection.
- **PHP Extension** – Exposes only `\mesi\parse(input, max_depth, default_url)`. No security configuration.
- **Caddy / FrankenPHP** – FrankenPHP uses the Caddy plugin, identical functionality.
- **Proxy** – Accepts full `EsiParserConfig`; all features available when provided by calling code.
- **IncludeErrorMarker (CLI)** – Can only be set programmatically (no CLI flag).
