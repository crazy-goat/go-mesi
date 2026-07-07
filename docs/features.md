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
| `IncludeErrorMarker` | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ⚠️ | ✅ |
| Global MaxDepth | ✅ | ✅ | ❌ | ✅ | ❌ | ❌ | ✅ | ✅ | ✅ |
| Global Timeout | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ |
| SSRF (BlockPrivateIPs) | ✅ | ⚠️ | 🔒 | ✅ | ✅ | ✅ | ✅ | 🔒 | ✅ |
| AllowedHosts | ✅ | ❌ | ❌ | ✅ | ❌ | ✅ | ❌ | ❌ | ✅ |
| AllowPrivateIPsForAllowedHosts | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| MaxResponseSize | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ |
| MaxConcurrentRequests | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ |
| MaxWorkers | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ |
| ParseOnHeader | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ |
| Debug mode | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ |
| Cache (in-memory) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Cache (Redis) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Cache (Memcached) | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Custom CacheKeyFunc | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ |
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
- **Nginx** – Uses `ParseWithConfig` from `libgomesi` (migrated from `Parse`). SSRF blocking is configurable via `mesi_block_private_ips` (default `on` — see BREAKING CHANGE in CHANGELOG). Supports `mesi_shared_http_client` for connection pooling. Cache backends: in-memory, Redis (`mesi_cache_redis_addr`, `mesi_cache_redis_password`, `mesi_cache_redis_db`), and Memcached (`mesi_cache_memcached_servers`).
- **PHP Extension** – Exposes `\mesi\parse(input, max_depth, default_url)` (legacy, no security configuration) and `\mesi\parse_with_config(input, max_depth, default_url, config)` which supports `block_private_ips` (defaults to `true` — SSRF dial-time blocking of private/reserved IP ranges). The legacy `parse()` entrypoint keeps the module-init behaviour (no blocking) until a `parse_with_config()` call opts in.
- **Caddy / FrankenPHP** – FrankenPHP uses the Caddy plugin, identical functionality.
- **Proxy** – Accepts full `EsiParserConfig`; all features available when provided by calling code.
- **IncludeErrorMarker (CLI)** – Can only be set programmatically (no CLI flag).
- **`fetch-mode="ab"` (`ab-ratio`)** – Format requirements enforced since #315:
  - Exactly one `:` separator (`A:B`); leading and trailing whitespace is trimmed.
  - Each side must be a non-negative unsigned integer.
  - Either side may be zero; both zero (`0:0`) is rejected because no traffic would reach `Alt`.
  - Each side must be ≤ `MaxABRatio` (1,000,000) — protects downstream arithmetic from silent overflow.
  - Malformed values (missing colon, extra colons, non-numeric, negative, decimal, oversized, both-zero) yield `*ErrInvalidABRatio`. The error surfaces through the existing include-error path: rendered as `IncludeErrorMarker` (empty by default), skipped if `onerror="continue"`, or replaced with the tag body if provided. Empty `ab-ratio` falls back to the documented default `50:50`.
- **`max-depth` (per-tag)** – Format requirements enforced since #317:
  - Must parse as a non-negative unsigned integer; leading and trailing whitespace is trimmed.
  - Must be ≤ `MaxMaxDepth` (10,000) — well above any realistic ESI recursion depth and far below any platform's `uint` wrap boundary, so the `uint(v)+1` clamp that strengthens the parent's `MaxDepth` can never overflow.
  - A validated override clamps the parent's `MaxDepth` to `v+1` ("override can only tighten, never widen" semantics, identical to the legacy contract). Explicit `max-depth="0"` therefore reduces the parent's `MaxDepth` to `1` (the historical "one more level" signal).
  - Malformed values (non-numeric, negative, decimal, oversized, beyond `MaxUint64`) yield `*ErrInvalidMaxDepth`. The error surfaces through the configured logger (Debug when running with `Debug: true` or the supplied `Logger` accepts `Warn`; no-op under the default `DiscardLogger`) and the override is dropped: the parent's `MaxDepth` survives untouched, preventing a single misconfigured include from silently disabling all nested ESI processing under it. Empty / whitespace-only `max-depth` is treated as "no override" and the parent's value is preserved verbatim.
- **PHP Extension – in-memory cache (`parse_with_config()`)** since #226:
  - Exposed via `mesi\parse_with_config($input, $max_depth, $default_url, $config)` where `$config` is an associative array.
  - `cache_backend` accepts only `"memory"` or absent/empty; any other string triggers `E_WARNING` and `parse_with_config()` returns `false` (the function never silently degrades to "no cache" on a typo). The "memory" backend is wired through libgomesi `InitCache` per PHP worker process.
  - `cache_size` must be a non-zero integer in `[1, 1_000_000]`; `cache_ttl` must be an integer in `[0, 86_400]` seconds. Out-of-range or wrong-type values produce `E_WARNING` plus `false`.
  - The extension caches the last `(backend, size, ttl)` tuple per worker process; subsequent calls with the same parameters skip `InitCache` so the existing in-process entries survive. Calling with different parameters (e.g. pointing the same worker at a larger size) wipes the cache by design — callers that need long-lived cache state therefore keep parameters consistent.
  - Cache is **per-worker-process**: each PHP-FPM/PHP-CLI worker has its own private cache; entries are not shared across workers. For cross-process / cross-host sharing, use the planned Redis and Memcached backends (#231, #235).
  - The legacy `mesi\parse($input, $max_depth, $default_url)` entrypoint is unchanged — it never touches the cache.
- **PHP Extension – Redis and Memcached cache backends (`parse_with_config()`)** since #231:
  - Both backends reuse the existing in-process `parse_with_config()` config array. The extension no longer routes through the old `InitCache` entry point; it now wires a JSON config blob through `libgomesi.InitCacheWithConfig(backend, size, ttl, configJSON)`.
  - For `cache_backend = "redis"` the extension renders `{"redisAddr":"host:port","redisPassword":"…","redisDB":N}`. `cache_redis_addr` is required, must be `"host:port"` with port in `[1, 65535]`, and rejects whitespace, control chars, `"` and `\` (so the rendered JSON cannot be broken by a hostile operator input — same restrictions as the Apache `MesiCacheRedisAddr` validator). `cache_redis_password` is optional and follows the same character rules. `cache_redis_db` accepts integers in `[0, 15]`; an explicit `0` is distinguished from "unset" so the rendered JSON omits the key in the latter case (matching libgomesi's "default 0" semantics). `cache_redis_addr` / `cache_redis_password` / `cache_redis_db` keys supplied with any other backend are rejected with `E_WARNING` (mismatched backend can never silently demote to "no cache").
  - For `cache_backend = "memcached"` the extension renders `{"servers":["h:p",…]}`. `cache_memcached_servers` is required and must be a non-empty array of `"host:port"` strings passing the same validator as `cache_redis_addr`. A non-string entry, an entry missing the port, an out-of-range port, an entry with whitespace / control / `"` / `\`, or supplying this key with a non-memcached backend all produce `E_WARNING` + `false`.
  - Init succeeds even when no Redis / Memcached daemon is reachable because the underlying `go-redis` and `gomemcache` clients are lazy; subsequent `<esi:include>` traffic will fall back to the origin server (degraded mode) and cache entries will simply never be observed. The same in-process `(backend, size, ttl, configJSON)` last-init state that already protects the `memory` backend now also caches the rendered JSON, so repeated `parse_with_config()` calls with identical Redis / Memcached configuration skip `InitCacheWithConfig` and never replace sharedCache with a fresh, empty instance.
- **Traefik `blockPrivateIPs` (#195)** — The option is configurable (default `true`) and wired through to `mesi.EsiParserConfig.BlockPrivateIPs`. Because the Traefik plugin runs under Yaegi, the dial-time IP-blocking transport (`ssrf_dialer.go`) is excluded and replaced by a stub `NewSSRFSafeTransport` (see `servers/traefik/Dockerfile`), so the option is exposed but the actual dial-time private-IP enforcement is **not active** in the interpreted plugin. Effective SSRF control in Traefik comes from the URL-level `allowedHosts` option (#200). The matrix therefore marks Traefik SSRF as ⚠️ (partial), not ✅.
- **RoadRunner memory cache (`cache_backend memory`)** since #236:
  - `cache_backend` accepts `"memory"` plus the build-tag-gated `"redis"` / `"memcached"`. Any unknown value is rejected by `initCache` so a typo never silently degrades to "no cache" (the caddy intermediate had the same constraint).
  - `cache_size` is bounded to `[1, 1_000_000]` through `normalizeCacheSize`. Values `<= 0` are kept as the documented "unset → use default 10 000 entries" sentinel (matching caddy / apache / cli / libgomesi behaviour). Values above `MaxCacheSize` are rejected at plugin `Init`, so a stray 5_000_000 no longer silently feeds the in-memory map constructor.
  - `cache_ttl` is parsed through `parseCacheTTL`: an empty string means "no explicit TTL" (default 0, no expiry), the string must be a non-negative Go duration, and the ceiling is `MaxCacheTTL` (24 h). Strings like `"-1s"` are rejected instead of flowing into `mesi.NewMemoryCache`, where they would silently translate to "no expiry" (`cache_memory.Set` treats `< 0` like `0`). Strings unparseable by `time.ParseDuration` keep the legacy `invalid cache_ttl …` error so operator typos surface during plugin startup.
  - Cache scope is **per RoadRunner worker goroutine**: each worker owns its own `MemoryCache`, dedups within that worker only. Cross-worker sharing requires the `redis` or `memcached` backend (both already supported).
  - The `examples/roadrunner-memory-cache.yaml` fixture ships a runnable `.rr.yaml` snippet with the documented bounds; the README `Cache backends → Memory` section still drives the discovery.
- **Apache `build.sh` (#102)** — Local builds (developers who prefer `./build.sh` over the Dockerfile) get loud errors instead of the legacy `cp … 2>/dev/null || sudo cp …` pattern that masked missing-source, read-only-prefix, missing-`apxs`, and missing-`go` failures. Concretely:
  - `set -euo pipefail` is enabled, so unset variables and pipe failures abort the script (the legacy `set -e` only caught the immediate exit status of `cmd1 || cmd2`).
  - Preflight writability check: `if [[ -w "$INSTALL_PREFIX" ]]` decides whether the install needs `sudo`; the legacy code's first `cp` always failed on a correctly-configured system and only the `sudo cp` was a real code path. The new script never invokes `sudo` when the prefix is writable.
  - `INSTALL_PREFIX` is overridable (defaults to `/usr/lib`); convenient for `/usr/local/lib` conventions. Set `LIBGOMESI_SO` to point at a pre-built `.so` to skip the inline `go build`.
  - Errors are not silenced: missing `libgomesi.so` plus missing `go` ⇒ two distinct error lines; missing `apxs` / `apxs2` ⇒ one clear error naming the missing Debian/RHEL package; non-writable prefix ⇒ the script prints the destination and "requesting sudo to install …" before invoking sudo, so the operator knows why the password prompt appeared.
  - `install -m 0644` replaces `cp`; the file mode is atomic and the call surface is well-defined.
  - A new test runner (`servers/apache/test_build_sh.sh`) exercises six deterministic scenarios with stub `apxs` / `sudo`, asserting both exit status and stderr content. The "real failure" scenarios cover a non-existent `INSTALL_PREFIX`, a missing `apxs` / `apxs2`, and a missing `libgomesi.so` + missing `go` — each maps to a specific error line that legacy `cp ... 2>/dev/null || sudo cp …` would have silenced.
