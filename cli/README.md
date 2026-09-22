# mESI command

A command-line interface (CLI) tool for experimenting with and testing ESI (Edge Side Includes) functionality. 
This CLI is built on top of the [go-mesi](https://github.com/crazy-goat/go-mesi/tree/cli) library, which extends Go’s HTTP capabilities for ESI parsing and rendering.

## Table of Contents
- [Overview](#overview)
- [Features](#features)
- [Installation](#installation)
- [Usage](#usage)
    - [Basic Commands](#basic-commands)
    - [Example Usage](#example-usage)


## Overview

The `mesi-cli` is designed to demonstrate how the [go-mesi](https://github.com/crazy-goat/go-mesi/tree/cli) library works and to provide developers with a straightforward way to:

1. Parse ESI markup in HTML documents.
2. Render or simulate server-side fragment assembly.
3. Test and debug ESI-related workflows.

This tool helps those new to ESI or the `go-mesi` library understand how to process ESI tags, retrieve fragments, and integrate them into one or more assembled pages.

## Features

- **ESI Tag Parsing**: Parses standard ESI tags (e.g., `<esi:include>`).
- **Local & Remote Fragment Retrieval**: Supports retrieving fragments from local file paths or remote URLs.
- **Command-Line Oriented**: Offers a simple CLI interface for quick tests without requiring a full web server environment.

## Installation

**Clone this repository** (or download it):
```bash
git clone https://github.com/crazy-goat/go-mesi.git
cd go-mesi/cli
make
```

## Usage
Basic Commands
Run the mesi-cli binary followed by the command and any required arguments:

```shell
mesi-cli [options] path/url
```

**Flags**
- **default-url <url>** (string): Specifies the default URL to parse when no explicit source is provided. Default: http://127.0.0.1/
- **max-depth <depth>** (integer): Defines the maximum depth of parsing, which can limit how many nested ESI includes or references are processed. Range `[0, 10000]` (`mesi.MaxMaxDepth`); values above the cap are rejected. Explicit `0` is passthrough. Default: 5
- **timeout <seconds>** (float): Sets the request timeout duration (in seconds) for all retrieval operations. Default: 10.0
- **parse-on-header** (bool): Enables ESI parsing on the HTTP headers, if set to `true` response must have `Edge-control: dca=esi` to enable parsing. Default: false
- **cache-backend <name>** (string): Cache backend for ESI includes. Values: `memory`, `redis`, `memcached`. Default: off (no caching)
- **cache-size <entries>** (int): Max cache entries for the memory backend. Default: 10000
- **cache-ttl <duration>** (duration): Cache TTL (e.g. `30s`, `5m`); `0` = no expiry. Default: 0
- **max-workers <count>** (int): Caps the per-`MESIParse` token-processing goroutine pool that drains `<esi:include>` jobs — one pool per invocation-level parse (each nested parse spawns its own pool and inherits the cap, so this is NOT process-wide across runs). Range `[0, 999999999]` (#171's transport-derived cap — `C int` / 9-digit `parse_nonneg_int`, mirrored from libgomesi's `config.MaxMaxWorkers`, which cannot be imported from this module); values outside the range — including negatives and `1000000000` — are rejected after flag parsing with an error naming the flag and the valid range (same contract as `max-depth`/`max-response-size`/`max-concurrent-requests`), non-integers are rejected by the flag parser. `0` = library default `runtime.NumCPU()*4` (the core substitutes it for any value ≤ 0 — the CLI rejects negatives instead of letting that substitution swallow a malformed explicit value silently, #456). Useful for forcing sequential processing (`-max-workers 1`) to make caching deterministic. Default: 0 (= `CreateDefaultConfig()`'s value, byte-identical to the behaviour before this flag existed; the CLI has no config-merge layer, so an absent flag and an explicit `0` are the same value here — unlike Apache, whose `-1` unset sentinel separates the two)
- **max-response-size <bytes>** (int64): Caps the HTTP response body size of a single `<esi:include>` fetch, in bytes (per-include, not per page). Range `[0, 9223372036854775806]` (`math.MaxInt64 - 1`); values outside the range — including negatives and `math.MaxInt64` — are rejected after flag parsing with an error naming the flag and the valid range (same contract as `max-depth`), non-integers are rejected by the flag parser. `0` = unlimited (the core only limits when the value is `> 0`). An over-limit include fails its fetch and renders empty output (or `include-error-marker` when set). Default: 10485760 (10 MB — `CreateDefaultConfig()`'s value, byte-identical to the behaviour before this flag existed; only an explicit value changes it)
- **max-concurrent-requests <count>** (int): Caps the number of concurrent `<esi:include>` HTTP fetches within a single invocation — one `MESIParse` call, so per run, NOT process-wide across invocations (each run builds its own config and admission semaphore). Range `[0, 999999999]`; values outside the range — including negatives and `1000000000` — are rejected after flag parsing with an error naming the flag and the valid range (same contract as `max-depth`/`max-response-size`), non-integers are rejected by the flag parser. `0` = unlimited (the core only installs the admission-control semaphore when the value is `> 0`); includes beyond the cap are queued and still delivered, never dropped. Default: 0 (unlimited — `CreateDefaultConfig()`'s value, byte-identical to the behaviour before this flag existed; only an explicit value changes it)
- **allow-private-ips** (bool): Allow ESI includes to private/reserved IP ranges. Required when testing against a local ESI origin. Default: false
- **allowed-hosts <hosts>** (string): Comma-separated list of allowed hosts for ESI includes. Only includes whose host is listed (exact or subdomain-suffix match with a `.` boundary that rejects suffix injection; case-insensitive; ports ignored) are fetched. Unset = all hosts allowed, subject to `allow-private-ips` (the whitelist check runs by hostname first and does NOT bypass the private-IP block). Separate hosts with commas only — whitespace around an entry is part of the entry and would prevent it from ever matching. Default: empty (no restriction)
- **allowPrivateIPsForAllowedHosts** (bool): When true, hosts listed in `allowed-hosts` may resolve to private/reserved IP ranges — the dial-time private-IP block is bypassed for them. Only effective when BOTH the private-IP block is active (`allow-private-ips` NOT set) AND `allowed-hosts` is non-empty — otherwise a no-op; unlisted hosts can never bypass (the whitelist check runs first). **Security warning: trusts DNS** for hosts in `allowed-hosts` — a compromised entry can reach internal/private addresses; only use with a DNS source you control. No effect together with `shared-http-client` (the shared transport bakes the private-IP policy at startup — same limitation as the server integrations). Default: false
- **shared-http-client** (bool): Share a single HTTP client (with connection pooling) across all ESI includes within a single invocation. When false (default), each include creates a fresh `http.Client`. Use this flag when processing a page with many includes to the same origin for measurable latency improvement. Default: false
- **cache-key-template <template>** (string): Custom cache key template with placeholders. Supported placeholders: `${url}` (the include URL). Example: `mesi:${url}:${header:Accept-Language}`. Note: header and cookie placeholders require an HTTP request context and are not supported in CLI mode (only `${url}` is substituted). Default: URL-only cache key.
- **include-error-marker <marker>** (string): Marker string rendered in place of a failed `<esi:include>` when no `onerror="continue"` and no fallback body is present. Useful for debugging — set to something like `"<!-- esi error -->"` to make failed includes visible in the rendered HTML. Security warning: never include the original error message as it may leak internal details. Default: "" (silent — failed includes produce empty output).

### Caching

The CLI exposes the in-memory, Redis, and Memcached caches from the `mesi` package. Repeated `<esi:include>` URLs within a single invocation are served from the cache instead of hitting the origin again.

```shell
# In-memory cache (per-invocation)
mesi-cli -cache-backend=memory -cache-size=5000 -cache-ttl=60s ./input.html

# Redis cache (persistent, shared)
mesi-cli -cache-backend=redis -cache-ttl=60s -cache-redis-addr=localhost:6379 ./input.html

# Memcached cache (persistent, shared)
mesi-cli -cache-backend=memcached -cache-ttl=60s -cache-memcached-servers=localhost:11211 ./input.html
```

The memory cache is **per-invocation** — it lives for the duration of a single `mesi-cli` run. Redis and Memcached caches are persistent and can be shared across invocations.

```shell
# Custom cache key template (URL-only placeholder supported in CLI mode)
mesi-cli -cache-backend=memory -cache-key-template='myapp:${url}' ./input.html
```

```shell
# Allow-list: only fetch includes from these hosts
mesi-cli -allowed-hosts="backend.internal,cdn.example.com" ./input.html
```

## Example Usage
Render an ESI-enabled HTML from a file:
```shell
mesi-cli ./examples/simple.html
```
This command will parse index.html for ESI tags, fetch any fragments, and then write the assembled document to stdout.


Render an ESI-enabled HTML from a remote source:
```shell
 ./mesi-cli https://raw.githubusercontent.com/crazy-goat/go-mesi/refs/heads/main/examples/index.html
```
This will fetch an HTML page with [simple example](../examples/index.html), parse its ESI tags, retrieve fragments (either remote or local), and save the rendered output to stdout.