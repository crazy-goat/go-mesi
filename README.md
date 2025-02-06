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

## Roadmap

### Servers Integration
✅ **Initial Implementation** – Basic support for ESI processing.  
🔄 **Upcoming Integrations:**
- [x] Plugin for Traefik - See [Installation and configuration](servers/traefik/README.md)
- [ ] Plugin for Caddy
- [ ] Plugin for RoadRunner
- [ ] Plugin for FrankenPHP
- [ ] Plugin for Nginx (if possible)
- [ ] Plugin for Apache (if possible)
- [ ] PHP extension (if possible)
- [ ] Standalone proxy server

### Features
🔄 **Caching Enhancements:**
- [ ] Add cache key option
- [ ] Add Redis cache support
- [ ] Add Memcached cache support

🔄 **Performance & Scalability:**
- [ ] Add work modes:
 - [x] Fallback
 - [ ] A/B testing with ratio
 - [ ] Concurrent fetching
- [ ] Add timeout parameter for ESI requests
- [ ] Add max concurrent request limit
- [ ] Implement worker pool for optimized request handling
- [ ] Implement relative include path

---

🚀 **Looking for contributors!** If you are interested in helping with development, feel free to submit PRs or open issues.  

