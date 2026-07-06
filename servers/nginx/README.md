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

A shared HTTP client is automatically created once per worker process and reused for all ESI fragment fetches. This reuses idle TCP connections (default: 100 per host, 90s idle timeout), dramatically reducing latency for pages with multiple `<esi:include>` tags to the same backend.

The shared client includes SSRF protection — connections to private/reserved IP addresses are blocked at dial time.

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
