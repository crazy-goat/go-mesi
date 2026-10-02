# ESI Proxy Server

A standalone reverse proxy server that processes ESI (Edge Side Includes) tags, useful for local development, testing, and as a lightweight deployment option without requiring a full web server integration.

## Features

- Reverse proxy with automatic ESI tag processing
- Full `EsiParserConfig` support (timeout, maxDepth, parseOnHeader)
- Configurable via CLI flags
- Graceful shutdown support

## Installation

```bash
# Build from source
make build

# Or run directly
go run . --backend http://your-backend:8081
```

## Usage

```bash
mesi-proxy --backend http://localhost:8081 --listen :8080
```

### CLI Options

| Flag | Default | Description |
|------|---------|-------------|
| `--listen` | `:8080` | Listen address |
| `--backend` | (required) | Upstream backend URL |
| `--max-depth` | `5` | Maximum recursion depth for ESI includes |
| `--timeout` | `10s` | Request timeout in seconds |
| `--parse-on-header` | `false` | Only parse when `Edge-control: dca=esi` header is present |
| `--block-private-ips` | `true` | Block private IP addresses in included URLs |
| `--debug` | `false` | Enable debug logging |
| `--max-body-size` | `0` | Max size in bytes of the parent HTML body buffered for ESI processing; `0` = unlimited (#538) |
| `--on-oversize` | `pass` | When the body is over `--max-body-size`: `pass` sends it unchanged and logs a warning, `error` logs an error and answers `502 Bad Gateway` |
| `--cache-backend` | `""` | Cache backend: `memory`, `redis`, `memcached` (default: off) |
| `--cache-size` | `10000` | Max cache entries for memory backend |
| `--cache-ttl` | `0` | Cache TTL (e.g. `30s`, `5m`); `0` = no expiry |
| `--cache-redis-addr` | `localhost:6379` | Redis server address (host:port) |
| `--cache-redis-password` | `""` | Redis password |
| `--cache-redis-db` | `0` | Redis database number |
| `--cache-memcached-servers` | `""` | Comma-separated Memcached servers (host:port) |

### Parent body limit

`--max-body-size` bounds the HTML page that the proxy holds in memory before it runs ESI on it. It does not limit single includes. Only `text/html` responses are counted (with `--parse-on-header`, only those with `Edge-control: dca=esi`, because the others are not parsed). If the upstream `Content-Length` is over the limit the decision is made at once; otherwise it is made when the buffered size crosses the limit. In `pass` mode the part buffered so far and the rest are streamed to the client unchanged. In `error` mode the rest is read and dropped (not buffered) and the client gets `502 Bad Gateway`. `HEAD` requests are never limited. A negative size or an unknown mode makes the proxy exit at start-up.

## Docker

```bash
# Build
make docker-build

# Run
make docker-run
```

Or manually:

```bash
docker build -t mesi-proxy .
docker run --rm -p 8080:8080 -e BACKEND=http://host.docker.internal:8081 mesi-proxy
```

## How It Works

1. Accepts incoming HTTP requests
2. Forwards them to the configured backend server
3. Checks if response is HTML (`Content-Type: text/html`)
4. If `ParseOnHeader` is enabled, checks for `Edge-control: dca=esi` header
5. Processes response through `mesi.MESIParse()` with configured `EsiParserConfig`
6. Returns processed response to client

## Example

Start a backend server:

```bash
cd ../test-server && go run test-server.go &
```

Start the proxy:

```bash
./mesi-proxy --backend http://127.0.0.1:8081
```

Test:

```bash
curl http://localhost:8080/
```

## Development

```bash
# Run tests
make test

# Clean build artifacts
make clean
```