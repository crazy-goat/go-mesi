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