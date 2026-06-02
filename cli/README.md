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
- **max-depth <depth>** (integer): Defines the maximum depth of parsing, which can limit how many nested ESI includes or references are processed. Default: 5
- **timeout <seconds>** (float): Sets the request timeout duration (in seconds) for all retrieval operations. Default: 10.0
- **parse-on-header** (bool): Enables ESI parsing on the HTTP headers, if set to `true` response must have `Edge-control: dca=esi` to enable parsing. Default: false
- **cache-backend <name>** (string): Cache backend for ESI includes. Values: `memory`, `redis`, `memcached`. Default: off (no caching)
- **cache-size <entries>** (int): Max cache entries for the memory backend. Default: 10000
- **cache-ttl <duration>** (duration): Cache TTL (e.g. `30s`, `5m`); `0` = no expiry. Default: 0
- **max-workers <count>** (int): Max concurrent ESI include goroutines. `0` = `NumCPU*4`. Useful for forcing sequential processing to make caching deterministic. Default: 0
- **allow-private-ips** (bool): Allow ESI includes to private/reserved IP ranges. Required when testing against a local ESI origin. Default: false

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