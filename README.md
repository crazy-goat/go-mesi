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
    defaultUrl string
    maxDepth   uint
    timeout    time.Duration
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
🔄 **Caching Enhancements:**
- [ ] Add cache key option
- [ ] Add Redis cache support
- [ ] Add Memcached cache support

🔄 **Performance & Scalability:**
- [x] Implement include path without host
- [x] Add timeout parameter for ESI requests
- [ ] Add work modes:
  - [x] Fallback
  - [ ] A/B testing with ratio
  - [ ] Concurrent fetching
- [ ] Add max concurrent request limit
- [ ] Implement worker pool for optimized request handling

---

🚀 **Looking for contributors!** If you are interested in helping with development, feel free to submit PRs or open issues.  

