# Apache HTTP Server mESI Module

Apache output filter module for mESI (Edge Side Includes) processing.

## Requirements

- Apache HTTP Server 2.4+ (MPM `prefork` recommended; `worker` and `event`
  require a mutex around `Parse()`)
- libgomesi.so (built from `libgomesi/`)

## Building

```bash
# Build libgomesi first
cd ../../libgomesi
go build -buildmode=c-shared -o libgomesi.so libgomesi.go
sudo cp libgomesi.so /usr/lib/

# Build Apache module
./build.sh
```

## Installation

```bash
sudo make install
```

## Configuration

```apache
LoadModule mesi_module modules/mod_mesi.so

EnableMesi on
MesiAllowedHosts backend raw.githubusercontent.com
MesiBlockPrivateIPs Off
MesiCacheBackend memory
MesiCacheSize 1000
MesiCacheTTL 60
```

### Directives

- `EnableMesi on|off` — Enable/disable ESI processing. Default: off.
- `MesiAllowedHosts host1 host2 …` — Space-separated list of hostnames
  allowed in `<esi:include src=…>`. Matches `isURLSafe` from libgomesi.
- `MesiBlockPrivateIPs on|off` — Enable/disable SSRF dial-time private-IP
  blocking. Default: On.
- `MesiCacheBackend memory` — Cache backend. Only `memory` is supported;
  any other value (including a typo) is rejected at startup so a misconfig
  never silently disables caching. Empty string explicitly disables
  caching. When unset, no cache.
- `MesiCacheSize N` — Max entries for the in-memory cache. Must be a
  positive integer in `[1, 1000000]`. Default: 10000.
- `MesiCacheTTL N` — TTL in seconds for cached entries. Must be in
  `[0, 86400]`. Default: 0 (no expiry).

### Custom libgomesi path

Override at compile time:

```bash
make LIBGOMESI_PATH=/opt/libgomesi.so
```

## Testing

```bash
docker compose up --build
./test.sh
```

## MPM Compatibility

| MPM | Status | Notes |
|-----|--------|-------|
| Prefork | ✅ Recommended | dlopen / libgomesi state per worker |
| Worker | ⚠️ Supported | Each thread holds its own libgomesi |
| Event | ⚠️ Supported | Same as Worker |

## How It Works

1. Registers as output filter in Apache's filter chain.
2. Intercepts responses with `Content-Type: text/html`.
3. Adds `Surrogate-Capability: ESI/1.0` header.
4. Buffers response body until complete.
5. Calls `InitCache(...)` from libgomesi once per worker process when
   `MesiCacheBackend memory` is configured (TTL/size from
   `MesiCacheTTL`/`MesiCacheSize`).
6. Processes the buffered body through `libgomesi.ParseWithConfig()`.
   Repeated `<esi:include>` URLs within TTL are served from the cache.
7. Returns processed HTML to client.
