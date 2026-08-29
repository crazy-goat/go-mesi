# PHP mESI Extension
This repository contains a PHP extension that wraps the mESI library, a lightweight Edge Side Includes (ESI) implementation written in Go. By integrating mESI’s functionalities, this extension brings minimal but correct ESI processing to your PHP-based environment.

## Requirements

To build this PHP extension, you need Golang and the necessary dependencies for compiling PHP extensions. Install them using the following command on Debian-based systems:
```
sudo apt-get update && sudo apt-get install -y \
    golang \
    php-dev \
    build-essential \
    autoconf \
    bison \
    re2c \
    libxml2-dev
```

## Installation

Clone this repository:
```
git clone https://github.com/crazy-goat/go-mesi.git
cd go-mesi
```

Before building the PHP extension, you must first compile and install the libgomesi library. To do this, execute the following commands:
```
cd libgomesi
make
sudo make install
```
This step ensures that the required Go-based library is available for the PHP extension to link against.

Now you can proceed with building the PHP mESI extension. Follow these steps:
```
cd php-ext
phpize
./configure
make
sudo make install
```
This will compile, test, and install the PHP extension, making it ready for use in your environment.

# Enabling extension

To enable the **mESI PHP extension** on **Debian** or **Ubuntu**, follow these steps:

```
echo "extension=mesi.so" | sudo tee /etc/php/$(php -r 'echo PHP_MAJOR_VERSION.".".PHP_MINOR_VERSION;')/mods-available/mesi.ini
```

Activate the extension using `phpenmod`:
```
sudo phpenmod mesi
```

If you are using PHP-FPM, restart the service:
```
sudo systemctl restart php-fpm
```

Check if the extension is loaded correctly:
```
php -m | grep mesi
```

Hello world! example script:
```php
echo \mesi\parse('<!--esi Hello, world!-->', 5, "http://127.0.0.1");
```

## Extended API: `parse_with_config()`

For caching, use `parse_with_config()` with an associative `config` array. All three cache backends (`memory`, `redis`, `memcached`) are exposed to PHP.

```php
$html = \mesi\parse_with_config(
    $input,
    5,                          // max_depth (recommended: 5)
    'http://edge.example.com/', // default URL for relative includes
    [
        'cache_backend' => 'memory',     // "memory" | "redis" | "memcached" | ""
        'cache_size'    => 5000,         // entries; default 10000, range [1, 1_000_000]
        'cache_ttl'     => 60,           // seconds; default 0 (no expiry), range [0, 86_400]
    ]
);
```

### Cache backends

| Key | Required when | Type | Notes |
|-----|---------------|------|-------|
| `cache_backend` | always | string | `""`, `"memory"`, `"redis"`, or `"memcached"` (any other value is rejected) |
| `cache_size` | optional | int | `[1, 1_000_000]`; `0` is treated as "use default 10000" on first init |
| `cache_ttl` | optional | int | `[0, 86_400]`; `0` means "no TTL" |
| `cache_redis_addr` | `cache_backend = "redis"` | string | `"host:port"` with port in `[1, 65535]`; no whitespace, control, `"` or `\` (the value is rendered into a JSON config blob) |
| `cache_redis_password` | optional, `cache_backend = "redis"` | string | Optional Redis AUTH password; same character restrictions as `cache_redis_addr` |
| `cache_redis_db` | optional, `cache_backend = "redis"` | int | `[0, 15]`; omitted means Redis DB 0 |
| `cache_memcached_servers` | `cache_backend = "memcached"` | array of strings | Each entry is `"host:port"`; non-empty list required |
| `block_private_ips` | optional | bool | SSRF dial-time blocking of private/reserved IP ranges. Defaults to `true` (secure by default); pass `false` to allow includes from private IPs (e.g. loopback, RFC1918). A non-boolean value is rejected with `E_WARNING` |
| `allowed_hosts` | optional | string | Space-separated hostname whitelist restricting which `<esi:include>` destinations are fetched (e.g. `'backend.internal cdn.example.com'`). Empty/absent = all hosts allowed (backward compatible). Non-string, control-character, or whitespace-only values are rejected with `E_WARNING` |
| `allow_private_ips_for_allowed_hosts` | optional | bool | Per-host private-IP bypass: when `true`, hosts listed in `allowed_hosts` may resolve to private/reserved IPs (the dial-time block is bypassed for them). Defaults to `false` (no bypass). Only effective when BOTH `block_private_ips` is `true` AND `allowed_hosts` is a non-empty whitelist; otherwise a no-op. **Trusts DNS** — only use with internal/trusted DNS. A non-boolean value is rejected with `E_WARNING` |
| `cache_key_template` | optional | string | Cache key template: `${url}`, `${header:Name}`, `${cookie:Name}`. Empty/absent = default URL-only key (`mesi.DefaultCacheKey`). Unknown placeholders stay literal. Case-insensitive header/cookie lookup via `mesi.BuildCacheKey`. Non-string / control-char / space-containing values are rejected with `E_WARNING`. Silently **ignored** when `cache_backend` is `""` (no cache — parity with CLI/Traefik #246). A template without `${url}` collapses all URLs to one entry |
| `request_headers` | optional | array | String keys (header names) → string or array-of-strings values. Keys and all values must pass `mesi_is_safe_string` (no control chars, space/tab, DEL, `"` or `\`). Empty array = no headers. Only rendered when a non-empty `cache_key_template` is set and `cache_backend != ""` |
| `request_cookies` | optional | array | String keys (cookie names, non-empty, no spaces/control) → string values (no control chars, `"` or `\`; spaces allowed in values). Empty array = no cookies. Only rendered when a non-empty `cache_key_template` is set and `cache_backend != ""` |

Validation is strict: an unknown `cache_backend`, mismatched Redis-vs-Memcached key, out-of-range numeric value, non-integer value, malformed `host:port`, or a non-string memcached server entry emits an `E_WARNING` and returns `false`. The function never silently degrades to "no cache" on a typo — a wrong host:port or empty memcached list surfaces as `E_WARNING`, matching the validation pattern in `parse_with_config()` for the in-memory backend and the equivalent `MesiCache*` directives in `servers/apache`. The same applies to `allowed_hosts`: a non-string or whitespace-only value is rejected (a whitespace-only list would silently tokenize to an empty allowlist = allow all hosts — the same fail-open typo nginx hardens against, #354). The legacy `\mesi\parse()` entrypoint is unchanged in its signature, but it shares the same per-process cache as soon as `\mesi\parse_with_config()` has been called at least once in this worker — don't rely on `\mesi\parse()` to bypass the cache.

### Examples

#### In-memory, per worker

```php
$esi = file_get_contents('template.html');
echo \mesi\parse_with_config(
    $esi,
    5,
    'http://edge.example.com/',
    ['cache_backend' => 'memory', 'cache_size' => 1000, 'cache_ttl' => 3600]
);
```

#### Redis (cross-worker / cross-host shared cache)

```php
echo \mesi\parse_with_config(
    $esi,
    5,
    'http://edge.example.com/',
    [
        'cache_backend'        => 'redis',
        'cache_size'           => 1000,
        'cache_ttl'            => 60,
        'cache_redis_addr'     => '10.0.0.5:6379',
        'cache_redis_password' => 's3cret',
        'cache_redis_db'       => 2,
    ]
);
```

#### Memcached (multiple servers for failover)

```php
echo \mesi\parse_with_config(
    $esi,
    5,
    'http://edge.example.com/',
    [
        'cache_backend'           => 'memcached',
        'cache_size'              => 1000,
        'cache_ttl'               => 120,
        'cache_memcached_servers' => ['10.0.0.1:11211', '10.0.0.2:11211'],
    ]
);
```

#### SSRF protection (block private IPs)

By default `parse_with_config()` blocks includes that resolve to private or
reserved IP ranges (loopback, RFC1918, CGNAT, link-local, etc.) at dial time,
preventing Server-Side Request Forgery via DNS rebinding. This is **secure by
default** — you must explicitly opt out:

```php
// Secure default: private IPs are blocked.
echo \mesi\parse_with_config(
    $esi,
    5,
    'http://edge.example.com/',
    ['block_private_ips' => true]
);

// Opt out (e.g. your origin lives on an internal/RFC1918 address):
echo \mesi\parse_with_config(
    $esi,
    5,
    'http://edge.example.com/',
    ['block_private_ips' => false]
);
```

The shared HTTP client's transport is rebuilt only when the requested
`block_private_ips` value changes between calls, so repeated calls with the
same setting incur no extra setup cost.

#### Host whitelist (`allowed_hosts`)

`allowed_hosts` restricts which `<esi:include>` destinations are fetched, by
hostname, before any connection is made. It complements `block_private_ips`:

```php
// Only includes to backend.internal and cdn.example.com are fetched.
echo \mesi\parse_with_config(
    $esi,
    5,
    'http://edge.example.com/',
    [
        'allowed_hosts'    => 'backend.internal cdn.example.com',
        'block_private_ips' => false,   // internal backend is on a private IP
    ]
);
```

Semantics (identical to Apache's `MesiAllowedHosts`, nginx's
`mesi_allowed_hosts` and libgomesi's `allowedHosts` parameter):

- Values are space-separated bare hostnames, passed verbatim to libgomesi.
- Matching is exact or subdomain-suffix: `sub.backend` matches `backend`.
- The `.` boundary prevents suffix injection: `notbackend.com` and
  `attacker-backend.com` do **not** match `backend`.
- Matching is case-insensitive; ports in include URLs are ignored
  (`http://backend:8000/` matches `backend`).
- Empty or absent value means **all hosts allowed** (backward compatible).
  Whitespace-only values (ASCII or Unicode) are rejected with `E_WARNING` —
  they would silently tokenize to an empty allowlist.
- `allowed_hosts` does **not** bypass `block_private_ips`: includes to
  private/reserved IPs still require `block_private_ips => false` (a
  dedicated `allow_private_ips_for_allowed_hosts` bypass is shipped since
  #196 — see below).
- The check runs by hostname before the dial-time private-IP check —
  two-phase defense-in-depth (redirect targets are re-validated on every
  hop by the shared core).

#### Private-IP bypass for whitelisted hosts (`allow_private_ips_for_allowed_hosts`)

Since #196. When `true`, hosts listed in `allowed_hosts` may resolve to
private/reserved IP addresses (the dial-time block is bypassed for them) —
the same security profile as Apache's `MesiAllowPrivateIPsForAllowedHosts`:

```php
// backend.internal is on a private IP; the whitelist lets it through
// while everything NOT in allowed_hosts stays blocked.
echo \mesi\parse_with_config(
    $esi,
    5,
    'http://edge.example.com/',
    [
        'allowed_hosts'                      => 'backend.internal',
        'block_private_ips'                  => true,
        'allow_private_ips_for_allowed_hosts' => true,
    ]
);
```

Semantics:

- Default `false` (no bypass) — backward compatible.
- Only effective when BOTH `block_private_ips` is `true` AND `allowed_hosts`
  is a non-empty whitelist naming the host; otherwise a no-op. Unlisted
  hosts are rejected by the whitelist before any dial, even with the option
  on.
- **Security warning: trusts DNS.** An attacker able to influence what an
  `allowed_hosts` entry resolves to (or to poison the resolver) can reach
  internal/private addresses. Use only with internal DNS (Consul,
  Kubernetes DNS, `/etc/hosts`).
- Per-parse shared-client detachment: because the shared HTTP client's
  transport bakes `block_private_ips` at startup, bypass-flagged parses use
  per-request HTTP clients (no connection pooling for those fetches). All
  other parses keep using the shared client unchanged.
- A non-boolean (or non-integer) value is rejected with `E_WARNING` and
  `parse_with_config()` returns `false` — a typo can never silently enable
  a security bypass.

#### Cache key template (`cache_key_template` + `request_headers` / `request_cookies`)

Since #238. When `cache_backend` is set, `cache_key_template` lets callers
vary cache keys by request metadata (headers, cookies) via placeholders
evaluated by `mesi.BuildCacheKey`:

- `${url}` — the `<esi:include src>` URL.
- `${header:Name}` — first value of request header `Name` (case-insensitive).
- `${cookie:Name}` — value of cookie `Name` (case-insensitive).
- Unknown placeholders (e.g. `${unknown:foo}`) are left literal.
- Empty or absent template falls back to the default URL-only key
  (`mesi.DefaultCacheKey`).
- A template without `${url}` collapses all URLs to a single cache entry.
- When `cache_backend` is `""` (no cache) the template is silently ignored
  — no warning (parity with CLI/Traefik #246).
- `request_headers` / `request_cookies` are only rendered into the Go
  request context when a non-empty template is active and `cache_backend != ""`.

```php
echo \mesi\parse_with_config(
    $esi,
    5,
    'http://edge.example.com/',
    [
        'cache_backend'      => 'memory',
        'cache_size'         => 1000,
        'cache_ttl'          => 60,
        'cache_key_template' => 'mesi:${url}:${header:Accept-Language}',
        'request_headers'    => ['Accept-Language' => 'pl'],
        // or array-of-strings: ['Accept-Language' => ['pl', 'en']]
        // 'request_cookies' => ['ab' => 'v1'], // matched by ${cookie:ab}
    ]
);
```

Validation is strict: `cache_key_template` must be a string passing
`mesi_is_safe_string` (no control chars, spaces, `"` or `\`);
`request_headers` keys/values and `request_cookies` keys/values are
validated the same way (cookie values allow spaces but still reject control
chars / `"` / `\`). Violations emit `E_WARNING` and return `false`.

`CacheKeyFunc` Go function pointers are **not** supported from PHP/C —
the template string is the only PHP-visible cache-key customization.

### Cache scope

- **In-memory (`memory`)** – per PHP worker process. Each worker has its own cache; entries are not shared across workers.
- **Redis (`redis`)** – shared across PHP workers and across hosts. Requires a reachable Redis server. Connection pooling is handled by the underlying `go-redis` client.
- **Memcached (`memcached`)** – shared across PHP workers and across hosts, with consistent hashing over the configured server list. Requires at least one reachable memcached daemon.

For the in-memory backend the cache lives inside `libgomesi` for the lifetime of one worker; for `redis` and `memcached` the same `libgomesi` shared cache is reused across repeated `parse_with_config()` calls within the worker (the extension tracks the last successful init so it does not call `InitCacheWithConfig` twice with the same parameters — that would otherwise drop every previously cached entry).
