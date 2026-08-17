# ESI middleware for roadrunner
A lightweight implementation of Edge Side Includes (ESI) middleware for RoadRunner

## Building RoadRunner with mESI middleware
To add the mesi middleware to the RoadRunner server, you need to compile it properly. The best way to do this is to use the [velox compiler](https://github.com/roadrunner-server/velox)

```shell
go install github.com/roadrunner-server/velox/v2024/cmd/vx@latest
```

Then you need to download the velox.toml file and add an entry for the mesi middleware to it
```toml
[github.plugins.mesi]
ref = "main"
owner = "crazy-goat"
repository = "go-mesi"
folder = "servers/roadrunner"
```

An alternative method is to use [this build script](build.sh):
```shell
./build.sh v2024.3.5
```
The script will download all dependencies and build RoadRunner with the mESI middleware.

## Configuration
To enable the mESI middleware, you must add the appropriate entry in the http module in the .rr.yaml configuration file.

```yaml
http:
  address: "0.0.0.0:8080"
  middleware:
    mesi:
```

### Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `max_depth` | int | `5` | Maximum ESI nesting depth. Set to `0` to disable ESI processing. |
| `shared_http_client` | bool | `false` | Reuse a single HTTP client for all ESI includes (SSRF-safe, connection pooling). |
| `block_private_ips` | bool | `true` | Block ESI includes to private/reserved IPs (loopback, RFC 1918, CGNAT, link-local, cloud metadata `169.254.169.254`, benchmark/documentation ranges) at dial time. Set `false` to allow internal includes (e.g. service meshes). |
| `allowed_hosts` | array | `[]` | Host whitelist restricting which ESI include destinations are fetched. Exact or subdomain-suffix match with a `.` boundary (rejects suffix injection); case-insensitive; ports ignored. Empty list = all hosts allowed (subject to `block_private_ips`). The whitelist check runs by hostname before the dial-time private-IP check and does NOT bypass it. |
| `allow_private_ips_for_allowed_hosts` | bool | `false` | When `true`, hosts listed in `allowed_hosts` may resolve to private/reserved IPs (the dial-time block is bypassed for them). Only effective when `block_private_ips` is `true` AND `allowed_hosts` is non-empty; no effect under `shared_http_client` (the shared transport bakes `block_private_ips` at startup). **Trusts DNS** — a compromised entry in `allowed_hosts` can reach internal/private addresses.
| `timeout` | string | `"10s"` | Maximum time for ESI processing (Go duration format). |
| `include_error_marker` | string | `""` | HTML marker rendered for failed includes (no `onerror="continue"`). |
| `cache_backend` | string | `""` | Cache backend: `""` (off), `"memory"`, `"redis"`, `"memcached"`. |
| `cache_size` | int | `10000` | Max entries for memory cache backend. |
| `cache_ttl` | string | `""` | Default TTL for cached entries, e.g. `"60s"`. |
| `cache_key_template` | string | `""` | Custom cache key template (see below). |
| `cache_redis_addr` | string | `"localhost:6379"` | Redis server address (host:port). |
| `cache_redis_password` | string | `""` | Redis AUTH password. |
| `cache_redis_db` | int | `0` | Redis database number. |
| `cache_memcached_servers` | array | `[]` | Memcached server addresses (host:port). |

#### Shared HTTP Client
Enables TCP connection reuse across ESI includes. The shared client uses an SSRF-safe transport that blocks private IPs.

```yaml
http:
  middleware:
    mesi:
      shared_http_client: true
```

#### Block Private IPs (SSRF protection)
By default, ESI includes to private/reserved IP ranges (loopback, RFC 1918, CGNAT, link-local, cloud metadata `169.254.169.254`, benchmark/documentation ranges) are blocked at dial time. Set `block_private_ips: false` only when you intentionally include from internal/private backends (e.g. an internal service mesh) and accept the SSRF exposure.

```yaml
http:
  middleware:
    mesi:
      block_private_ips: true
```

#### Allowed Hosts (SSRF protection)
Restricts ESI includes to a host whitelist. Matching is exact or subdomain-suffix (`sub.backend` matches `backend`); the `.` boundary prevents suffix injection (`notbackend.com` does NOT match `backend`); ports are ignored. Empty/absent list allows all hosts (backward compatible, still subject to `block_private_ips`). The whitelist check runs by hostname before the dial-time private-IP check and does NOT bypass it — includes to private/reserved IPs still require `block_private_ips: false`.

```yaml
http:
  middleware:
    mesi:
      allowed_hosts:
        - backend.internal
        - cdn.trusted.com
```

#### Allowed Hosts with private-IP bypass

For service meshes, `allow_private_ips_for_allowed_hosts: true` lifts the
SSRF block for hosts that are BOTH listed in `allowed_hosts` AND resolve to a
private/reserved IP. The hostname whitelist still applies — hosts outside
`allowed_hosts` are blocked regardless of this flag. Only effective when
`block_private_ips` is `true` (default) and `allowed_hosts` is non-empty.

> **Security warning:** this option trusts DNS. Anyone who can control which
> hostname an `<esi:include>` targets (or who can influence what an entry in
> `allowed_hosts` resolves to) can reach internal/private addresses. Only
> enable it when you control every hostname in `allowed_hosts`.

> **Interaction with `shared_http_client`:** the shared client's SSRF-safe
> transport is built once at startup and bakes in `block_private_ips`; the
> per-host bypass does NOT apply to shared-client fetches. Disable
> `shared_http_client` to use the bypass.

```yaml
http:
  middleware:
    mesi:
      allowed_hosts:
        - backend.internal   # resolves to 10.x.x.x
      block_private_ips: true
      allow_private_ips_for_allowed_hosts: true  # backend.internal may hit 10.x.x.x
```

### Cache backends

#### Memory
```yaml
http:
  middleware:
    mesi:
      cache_backend: memory
      cache_size: 5000
      cache_ttl: "60s"
```

#### Memcached
Requires building with `-tags memcached`:

```shell
go build -tags memcached ./...
```

```yaml
http:
  middleware:
    mesi:
      cache_backend: memcached
      cache_ttl: "120s"
      cache_memcached_servers:
        - "10.0.0.1:11211"
        - "10.0.0.2:11211"
```

#### Redis
Requires building with `-tags redis`:
```shell
go build -tags redis ./...
```

```yaml
http:
  middleware:
    mesi:
      cache_backend: redis
      cache_ttl: "120s"
      cache_redis_addr: "10.0.0.5:6379"
      cache_redis_db: 2
```

#### Cache Key Template
Customize cache keys based on request headers or cookies. Useful for multi-language sites or A/B testing.

```yaml
http:
  middleware:
    mesi:
      cache_backend: memory
      cache_ttl: "60s"
      cache_key_template: "mesi:${url}:${header:Accept-Language}"
```

**Template placeholders:**
- `${url}` — the include URL
- `${header:Name}` — request header value (supports canonical, lowercase, uppercase forms)
- `${cookie:Name}` — cookie value (supports canonical, lowercase, uppercase forms)

Unknown placeholders are left as literals.

An example script with the appropriate configuration can be found in the [worker](worker) directory