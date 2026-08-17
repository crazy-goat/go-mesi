# Nginx ESI module

This repository contains a Nginx module that wraps the mESI library, a lightweight Edge Side Includes (ESI) implementation written in Go. 
By integrating mESI’s functionalities, this module brings minimal but correct ESI processing to Nginx server.

## Requirements

To build this PHP extension, you need Golang and the necessary dependencies for compiling PHP extensions. Install them using the following command on Debian-based systems:
```
sudo apt-get update && sudo apt-get install -y \
    golang \
    build-essential \
    autoconf \
    bison \
    re2c \
    libxml2-dev \
    zlib1g-dev
```

## Installation

Clone this repository:
```
git clone https://github.com/crazy-goat/go-mesi.git
cd go-mesi
```

Before building the Nginx module, you must first compile and install the libgomesi library. To do this, execute the following commands:
```
cd libgomesi
make
sudo make install
```
This step ensures that the required Go-based library is available for the Nginx module to link against.

Now you can proceed with building the PHP mESI extension. Follow these steps:
```
cd servers/nginx
./build.sh
```

If the build goes well, you will find the nginx module file in this path: 
```
build/nginx/modules/ngx_http_mesi_module.so
```

# Enabling module

To enable the mESI module, add the following line to the main Nginx configuration file (e.g., nginx.conf):

```nginx configuration
load_module modules/ngx_http_mesi_module.so;
```

To enable the mESI module for a specific location in the HTTP server, add the following option:
```nginx configuration
enable_mesi on;
```
to the location section of the server configuration. For example:
```nginx configuration
location / {
    enable_mesi on;
    root   ../../tests;
    index  index.html;
}
```

## Shared HTTP Client

Not available in the nginx module: there is no shared-client directive (unlike Apache's `MesiSharedHTTPClient` and the CLI's `-shared-http-client`) and no `InitHTTPClient` wiring. Each `<esi:include>` fetch creates its own `http.Client` for the request, so TCP/TLS connection pooling across includes is not available — every include performs its own connection setup. Each per-include client still uses the SSRF-safe transport, so `mesi_block_private_ips` protection applies to every fetch.

## SSRF Protection

The `mesi_block_private_ips` directive controls whether ESI includes to private/reserved IP addresses (RFC 1918, CGNAT, link-local, loopback, benchmark and documentation ranges) are blocked at dial time. When enabled, an `<esi:include>` targeting e.g. `http://127.0.0.1/`, `http://169.254.169.254/` (cloud metadata) or any internal network address is rejected and rendered through the include-error path.

> **BREAKING CHANGE**: The default is now `on`. Previously nginx had **no** SSRF protection (implicit `off`), so any `<esi:include>` to a private IP succeeded. After upgrading, deployments that intentionally include from internal/private backends must explicitly set `mesi_block_private_ips off;`.

### Directive

#### `mesi_block_private_ips`

- **Syntax:** `mesi_block_private_ips on | off`
- **Default:** `on`
- **Context:** `location`

Enable (`on`, default) or disable (`off`) blocking of ESI includes to private/reserved IP addresses.

### Example

```nginx
location / {
    enable_mesi on;
    mesi_block_private_ips on;   # secure default; set off only for trusted internal includes
    proxy_pass http://backend;
}
```

## AllowedHosts

The `mesi_allowed_hosts` directive restricts which hosts `<esi:include src=…>` may
fetch, closing the SSRF gap for public-host exfiltration even when
`mesi_block_private_ips` is disabled. Both checks are independent and
complementary: `mesi_allowed_hosts` is validated by **hostname** before any
connection is attempted, `mesi_block_private_ips` runs at **dial time** on the
resolved IP address. A compromised backend can therefore never include from a
host outside the whitelist, regardless of the dial-time setting.

### Directive

#### `mesi_allowed_hosts`

- **Syntax:** `mesi_allowed_hosts <hosts>`
- **Default:** empty (no restriction)
- **Context:** `location`

Space-separated list of hostnames allowed in `<esi:include src=…>`. The list is
one nginx argument, so multiple entries are written as a single **quoted** string
(e.g. `mesi_allowed_hosts "backend.internal cdn.example.com";`) — mirroring the
existing `mesi_cache_memcached_servers` convention in this module. The content
format (space-separated hostnames) matches Apache's `MesiAllowedHosts` directive
for cross-server consistency.
Unset (empty) = no hostname restriction — backward compatible, subject to
`mesi_block_private_ips`.
Whitespace-only values are rejected at config load: the check mirrors
libgomesi's `strings.Fields` tokenization and covers the full Unicode
whitespace set (e.g. a non-breaking space U+00A0), so a value that would
tokenize to zero hostnames can never silently disable the restriction
(`nginx -t` fails). Values with leading/trailing whitespace around real
hostnames are fine.

### Matching semantics

A host matches if it is **exact** or a **subdomain suffix** of an entry:

- `example.com` matches `example.com` and `sub.example.com` (suffix `".example.com"`)
- `example.com` does **NOT** match `attacker-example.com` or `notexample.com`
  (no dot boundary — suffix-injection is rejected by libgomesi)
- Ports are ignored: `http://backend:8000/` matches entry `backend`

Multiple hosts are space-separated; extra whitespace between entries is fine.

### Redirects and relative includes

Redirect responses (301/302/303/307/308) are **not** followed automatically.
The shared core follows up to 10 redirects manually and re-validates every
hop target against `mesi_allowed_hosts` (plus the http/https scheme check)
before dialing it — an allowlisted backend can therefore never redirect to a
host outside the whitelist; the include fails and renders through the
include-error / fallback path.

Relative `<esi:include src="…">` paths resolve against the request's `Host`
header (via `DefaultUrl`, built as `scheme://Host/` by this module). When
`mesi_allowed_hosts` is set, the **resolved** host is subject to the whitelist
in the shared core: a relative include only works when the host the request
was made to is allowlisted.

### Example

```nginx
location / {
    enable_mesi on;
    mesi_block_private_ips off;   # trusted internal backend on a private IP
    mesi_allowed_hosts "backend.internal cdn.example.com";
    proxy_pass http://backend;
}
```

## AllowPrivateIPsForAllowedHosts

The `mesi_allow_private_ips_for_allowed` directive lets hosts listed in
`mesi_allowed_hosts` resolve to private/reserved IP addresses — the dial-time
`mesi_block_private_ips` block is bypassed **only for them**. Operators in
service meshes / internal networks no longer face the false dichotomy of
"block all private IPs" (breaks internal includes) vs "disable SSRF
protection entirely" (exposes everything).

> **SECURITY WARNING**: this trusts DNS for the hosts in `mesi_allowed_hosts`.
> If an attacker can control DNS for a listed host, they can resolve it to a
> private IP (e.g. `169.254.169.254`) and bypass SSRF protection. Use only
> with internal DNS you control (Consul, Kubernetes DNS, `/etc/hosts`).

### Directive

#### `mesi_allow_private_ips_for_allowed`

- **Syntax:** `mesi_allow_private_ips_for_allowed on | off`
- **Default:** `off` (private IPs always blocked regardless of
  allowed-host membership)
- **Context:** `location`

Hosts listed in `mesi_allowed_hosts` may resolve to private/reserved IP
addresses (the dial-time block is bypassed for them).

The bypass is **only** effective when BOTH `mesi_block_private_ips on` AND a
non-empty `mesi_allowed_hosts` are configured; otherwise it is a no-op:

- `mesi_block_private_ips off` → there is no dial-time block to bypass
- empty `mesi_allowed_hosts` → no host qualifies for the bypass
- hosts NOT in `mesi_allowed_hosts` are always subject to the dial-time
  block regardless of this directive — the whitelist check runs first (an
  unlisted host is rejected pre-dial)

Fallback: with a libgomesi build lacking the `ParseWithConfigEx` symbol
(older than the Apache #168 release), the directive is a no-op and a warning
is logged that the bypass is disabled; parsing continues with
`ParseWithConfig` semantics.

### Example

```nginx
location / {
    enable_mesi on;
    mesi_block_private_ips on;              # secure default stays on
    mesi_allowed_hosts "backend.internal"; # trusted internal backend
    mesi_allow_private_ips_for_allowed on;  # ... but allow it on its private IP
    proxy_pass http://backend;
}
```

## Cache Backend

The nginx module supports in-memory caching of ESI fragment responses. When enabled, duplicate `<esi:include>` URLs within the configured TTL are served from cache instead of fetching from the origin backend.

**Important limitation**: The in-memory cache is per-worker-process. Different nginx worker processes do **not** share cached entries. This is consistent with nginx's shared-nothing architecture.

### Directives

#### `mesi_cache_backend`

- **Syntax:** `mesi_cache_backend memory | redis | memcached | off`
- **Default:** `off`
- **Context:** `location`

Enables the LRU cache. Use `memory` for an in-process in-memory cache (per-worker, not shared across workers). Use `redis` or `memcached` for a shared external cache — requires the corresponding `mesi_cache_redis_*` or `mesi_cache_memcached_servers` directive.

#### `mesi_cache_size`

- **Syntax:** `mesi_cache_size <number>`
- **Default:** `10000`
- **Context:** `location`

Maximum number of cache entries. When the cache is full, the least-recently-used entry is evicted. Only applies to the `memory` backend, Redis and Memcached backends ignore this setting.

#### `mesi_cache_ttl`

- **Syntax:** `mesi_cache_ttl <seconds>`
- **Default:** `30`
- **Context:** `location`

Time-to-live in seconds for cached entries. After TTL expiry, the next request hits the origin and refreshes the cache.

#### `mesi_cache_memcached_servers`

- **Syntax:** `mesi_cache_memcached_servers <servers>`
- **Default:** `""`
- **Context:** `location`

Space-separated list of Memcached servers in `host:port` format (e.g., `"10.0.0.1:11211 10.0.0.2:11211"`). Required when `mesi_cache_backend` is `memcached`. An empty value with the memcached backend produces a deterministic error from libgomesi rather than silently defaulting to `localhost:11211`.

**Important**: Memcached has a 1 MB value size limit. ESI includes larger than 1 MB cannot be cached.

#### `mesi_cache_redis_addr`

- **Syntax:** `mesi_cache_redis_addr <address>`
- **Default:** `"localhost:6379"`
- **Context:** `location`

Redis server address in `host:port` format. Required when `mesi_cache_backend` is `redis`. If unset, defaults to `localhost:6379`.

#### `mesi_cache_redis_password`

- **Syntax:** `mesi_cache_redis_password <password>`
- **Default:** `""` (no password)
- **Context:** `location`

Redis server password. If unset, no password is sent.

#### `mesi_cache_redis_db`

- **Syntax:** `mesi_cache_redis_db <number>`
- **Default:** `0`
- **Context:** `location`

Redis database number (0–15). Defaults to 0 if not set.

### Example

```nginx
location / {
    enable_mesi on;
    mesi_cache_backend memory;
    mesi_cache_size 5000;
    mesi_cache_ttl 60;
    proxy_pass http://backend;
}
```

### Memcached Example

```nginx
location / {
    enable_mesi on;
    mesi_cache_backend memcached;
    mesi_cache_ttl 60;
    mesi_cache_memcached_servers "10.0.0.1:11211 10.0.0.2:11211";
    proxy_pass http://backend;
}
```

### Redis Example

```nginx
location / {
    enable_mesi on;
    mesi_cache_backend redis;
    mesi_cache_ttl 60;
    mesi_cache_redis_addr "10.0.0.5:6379";
    mesi_cache_redis_db 2;
    proxy_pass http://backend;
}
```

### Redis with Password Example

```nginx
location / {
    enable_mesi on;
    mesi_cache_backend redis;
    mesi_cache_ttl 60;
    mesi_cache_redis_addr "10.0.0.5:6379";
    mesi_cache_redis_password "your-redis-password";
    mesi_cache_redis_db 0;
    proxy_pass http://backend;
}
```

### Memory Usage

Estimated memory: `cache_size × average_include_body_size`. A 10,000-entry cache with 10 KB average entries uses ~100 MB. Plan capacity accordingly.

[Here](nginx.conf) you can find full example configuration
