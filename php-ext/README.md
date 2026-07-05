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

Validation is strict: an unknown `cache_backend`, mismatched Redis-vs-Memcached key, out-of-range numeric value, non-integer value, malformed `host:port`, or a non-string memcached server entry emits an `E_WARNING` and returns `false`. The function never silently degrades to "no cache" on a typo — a wrong host:port or empty memcached list surfaces as `E_WARNING`, matching the validation pattern in `parse_with_config()` for the in-memory backend and the equivalent `MesiCache*` directives in `servers/apache`. The legacy `\mesi\parse()` entrypoint is unchanged in its signature, but it shares the same per-process cache as soon as `\mesi\parse_with_config()` has been called at least once in this worker — don't rely on `\mesi\parse()` to bypass the cache.

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

### Cache scope

- **In-memory (`memory`)** – per PHP worker process. Each worker has its own cache; entries are not shared across workers.
- **Redis (`redis`)** – shared across PHP workers and across hosts. Requires a reachable Redis server. Connection pooling is handled by the underlying `go-redis` client.
- **Memcached (`memcached`)** – shared across PHP workers and across hosts, with consistent hashing over the configured server list. Requires at least one reachable memcached daemon.

For the in-memory backend the cache lives inside `libgomesi` for the lifetime of one worker; for `redis` and `memcached` the same `libgomesi` shared cache is reused across repeated `parse_with_config()` calls within the worker (the extension tracks the last successful init so it does not call `InitCacheWithConfig` twice with the same parameters — that would otherwise drop every previously cached entry).
