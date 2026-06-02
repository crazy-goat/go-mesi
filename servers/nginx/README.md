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

- **Syntax:** `mesi_cache_backend memory | off`
- **Default:** `off`
- **Context:** `location`

Enables the in-memory LRU cache. Currently only `memory` is supported. Redis and Memcached backends will be added in future releases.

#### `mesi_cache_size`

- **Syntax:** `mesi_cache_size <number>`
- **Default:** `10000`
- **Context:** `location`

Maximum number of cache entries. When the cache is full, the least-recently-used entry is evicted.

#### `mesi_cache_ttl`

- **Syntax:** `mesi_cache_ttl <seconds>`
- **Default:** `30`
- **Context:** `location`

Time-to-live in seconds for cached entries. After TTL expiry, the next request hits the origin and refreshes the cache.

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

### Memory Usage

Estimated memory: `cache_size × average_include_body_size`. A 10,000-entry cache with 10 KB average entries uses ~100 MB. Plan capacity accordingly.

[Here](nginx.conf) you can find full example configuration
