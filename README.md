# mESI – Minimal Edge Side Includes Implementation in Golang

**mESI** (**minimal Edge Side Includes**) is a lightweight implementation of **Edge Side Includes (ESI)** in Golang, designed to add ESI support to multiple web servers. It provides basic but correct handling of the following ESI instructions:

- `<esi:include>` – dynamic content inclusion,
- `<esi:remove>` – removal of specified sections,
- `<esi:comment>` – comments invisible to the end user,
- `<!--esi ... -->` – inline ESI processing.

## Features

- **Parallel Fetching** – Unlike many other ESI implementations, **mESI** supports parallel fetching of ESI fragments, improving response times for dynamic content.
- **Lightweight & Minimal** – Focuses on essential ESI features while remaining easy to integrate and extend.
- **Multi-Server Support** – Can be integrated with various web servers to enhance content delivery performance.

## ESI Parser Configuration
This document describes the configuration structure for the mESI parser.

### Configuration Structure
The parser configuration is defined using the following structure:

```go
type EsiParserConfig struct {
  DefaultUrl    string
  MaxDepth      uint
  Timeout       time.Duration
  ParseOnHeader bool
}
```
### Configuration Parameters
**DefaultUrl**

Base URL that will be used as a prefix for relative URL paths. If the provided URL in ESI tags doesn't start with "http://" or "https://", this base URL will be prepended to paths starting with "/".

**MaxDepth**

Defines the maximum allowed recursion depth for esi:include tags. This parameter prevents infinite loops that could occur when ESI templates reference each other.
The recursion count value can be lowered for a selected `esi:include` tag using the `max-depth` attribute:
```html
<esi:include max-depth="1" src="http://foo.bar/recursive"/>
```

**Timeout**

Specifies the maximum time to wait for a server response when processing esi:include tags. The request will be terminated if this timeout is exceeded.
Timeout can also be defined independently in the `esi:include` tag. The timeout attribute value is given in seconds. 
Decimal values can be given, the decimal separator is a dot, e.g. `1.2`

```html
<esi:include timeout="0.2" src="http://foo.bar/some-long request" />
```
_NOTE:_
 - If the `alt` attribute is provided and first request fails the time budget will be split between both requests.
 - In case of recursion, the timeout value is reduced by the time it took to execute the previous step.
 - When a timeout value is set in both `EsiParserConfig` and `esi:include`, the smaller value will be chosen.

**ParseOnHeader**

If set to true, then server responses will process ESI tags only when the response contains the `Edge-control: dca=esi` header

**fetch-mode** - `esi:include` tag only

Allows you to choose between three content download modes:
- fallback - default way to download content. In case of an error downloading from the first location (`src` attribute), 
the content will be downloaded from an alternative address (`alt` attribute). 
The `alt` attribute is not mandatory. This mode is selected if the `fetch-mode` attribute is missing.
- ab - Allows to download with different probability from two different locations src and alt. In case of no alt attribute, the src location will always be downloaded. 
The proportions of A and B are specified using the `ab-ratio` attribute, and it has the form X:Y where X and Y are positive integers. 
In case of no attribute or an incorrect value, the default proportion is 50:50.
- concurrent - both locations are fetched at the same time, but the result is returned from the fastest location. 
If the alt attribute is missing, the same location will be called twice.

**ab-ratio** - `esi:include` tag only

Used only when fetching data when `fetch-mode` is set to `ab`. Specifies the proportion of fetches from `src` to `alt`. 
Given in the form `X:Y`, where `X` and `Y` are non-negative integers. 
For example, a value of `90:10` specifies that `10%` of all queries will be fetched from `alt` source.
In case of no attribute `ab-ratio` or an incorrect value, the default proportion is `50:50`.
If missing `alt` attribute, `src` will be always used.

```html
<esi:include fetch-mode="ab" ab-ratio="90:10" src="http://foo.bar/A" alt="http://foo.bar/B" />]
```
I this case `B` will be fetched 10% of the time.

## Roadmap

### Servers Integration
✅ **Initial Implementation** – Basic support for ESI processing.  
🔄 **Upcoming Integrations:**
- [x] Plugin for Traefik - See [Installation and configuration](servers/traefik/README.md)
- [x] PHP extension - See [Installation and configuration](php-ext/README.md)
- [x] Plugin for Nginx - - See [Installation and configuration](servers/nginx/README.md)
- [ ] Plugin for Caddy
- [ ] Plugin for RoadRunner
- [ ] Plugin for FrankenPHP
- [ ] Plugin for Apache (if possible)
- [ ] Standalone proxy server

### Features
🔄 **Performance & Scalability:**
- [x] Implement include path without host
- [x] Add timeout parameter for ESI requests
- [x] Option to parse esi only when `Edge-control: dca=esi` header
- [x] Add work modes:
  - [x] Fallback
  - [x] A/B testing with ratio
  - [x] Concurrent fetching
- [ ] Add max concurrent request limit
- [ ] Implement worker pool for optimized request handling

🔄 **Caching Enhancements:**
- [ ] Add in memory cache
- [ ] Add cache key option
- [ ] Add Redis cache support
- [ ] Add Memcached cache support
---

## Running tests
To run E2E test just type `make test-e2e`

🚀 **Looking for contributors!** If you are interested in helping with development, feel free to submit PRs or open issues.  

