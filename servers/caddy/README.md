# ESI middleware for Caddy
A lightweight implementation of Edge Side Includes (ESI) middleware for Caddy server

## Building Caddy with mESI middleware
To add the mesi middleware to the Caddy server, you need to compile it properly. 
The best way to do this is to use the [xcaddy compiler](https://github.com/caddyserver/xcaddy)

```shell
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
```

Then just run the command below
```shell
xcaddy build --with github.com/crazy-goat/go-mesi/servers/caddy
```

Then we can check if caddy contains the right module using this command

```shell
caddy list-modules | grep mesi
```

this command should return 

```
http.handlers.mesi
```

## Configuration
Then you need to disable the mESSI middleware for that server.
You also need to set the appropriate order of the handlers using the order directive.

```
{
    order mesi before file_server
}

:8080 {
    root * ../../examples
    mesi
    file_server
}
```

Finally, you can start the Caddy server with the command:

```shell
caddy run --config Caddyfile
```

## Directives

### `shared_http_client`

Enables TCP connection reuse for ESI `<esi:include>` fetches.  
Without this, each include creates a fresh `http.Client` + `http.Transport`, incurring
N × TCP+TLS handshake overhead.

```
mesi {
    shared_http_client
}
```

The shared transport is created once at config load (in `Provision()`) and reused
for all requests. It uses `mesi.NewSSRFSafeTransport()` for dial-level SSRF
protection (private IPs are blocked).

Note: If adding Caddyfile directives that affect transport behaviour (e.g.
`block_private_ips`, `allowed_hosts`), `Provision()` must recreate the
shared transport to respect the new settings.

### `cache_backend memory`

Enables an in-process LRU cache for ESI fragments. Shared-nothing — each Caddy
instance has its own cache.

```
mesi {
    cache_backend memory
    cache_size 10000       # optional, default 10000
    cache_ttl 60s          # optional, default no expiry
}
```

| Subdirective | Description |
|---|---|
| `cache_size` | Max entries in the LRU cache. Default: 10000. |
| `cache_ttl` | Duration string (`60s`, `5m`, `1h`). Default: no expiry. |

### `cache_backend redis`

Enables a Redis-backed cache shared across Caddy instances. Ideal for
horizontally scaled deployments.

```
mesi {
    cache_backend redis
    cache_redis_addr   10.0.0.5:6379
    cache_redis_password s3cret   # optional
    cache_redis_db     2           # optional, default 0
    cache_ttl          120s        # optional, default no expiry
}
```

| Subdirective | Description |
|---|---|
| `cache_redis_addr` | Redis server address as `host:port`. Required. Default: `localhost:6379`. |
| `cache_redis_password` | Redis AUTH password. Optional. |
| `cache_redis_db` | Redis database number. Optional. Default: 0. |
| `cache_ttl` | Duration string (`60s`, `5m`, `1h`). Optional. Default: no expiry. |

**Notes:**
- `go-redis` pools connections internally. No extra pool configuration needed.
- Password in Caddyfile — ensure proper file permissions (e.g. `chmod 600`).
- Key prefix: `mesi:<url>`.
- Redis unreachable → ESI falls back to origin fetch (degraded, no crash).

### `cache_backend memcached`

Enables a Memcached-backed cache shared across Caddy instances. Ideal for
horizontally scaled deployments where Memcached is available.

```
mesi {
    cache_backend memcached
    cache_memcached_servers 10.0.0.1:11211 10.0.0.2:11211
    cache_ttl 120s
}
```

| Subdirective | Description |
|---|---|
| `cache_memcached_servers` | Space-separated list of `host:port` addresses. Required. |
| `cache_ttl` | Duration string (`60s`, `5m`, `1h`). Optional. Default: no expiry. |

**Notes:**
- Multiple servers are supported — the client distributes keys across them.
- Memcached has a 1 MB value size limit.
- Key prefix: `mesi:<url>`.
- Memcached unreachable → ESI falls back to origin fetch (degraded, no crash).

### `cache_key_template`

Custom cache key template with placeholders. Available for all cache backends.

```
mesi {
    cache_backend memory
    cache_key_template "mesi:${url}:lang=${header:Accept-Language}"
}
```

| Placeholder | Description |
|---|---|
| `${url}` | Full URL of the ESI include |
| `${header:Name}` | Request header value (case-insensitive) |
| `${cookie:Name}` | Request cookie value (case-insensitive) |

When unset, the URL-only default key is used.