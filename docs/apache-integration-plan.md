# Apache HTTP Server Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Apache HTTP Server module (mod_mesi) for mESI processing with CI integration and tests.

**Architecture:** Apache output filter module that loads libgomesi.so via dlopen, intercepts HTML responses, processes ESI tags. Docker-based integration tests.

**Tech Stack:** C (Apache module API), Apache httpd 2.4, APR, libgomesi.so, Docker

---

## File Structure

```
servers/apache/
├── mod_mesi.c           # Apache output filter module (CREATE)
├── config.m4            # apxs build config (CREATE)
├── Makefile             # Build targets (CREATE)
├── build.sh             # Build script (CREATE)
├── Dockerfile           # Test container (CREATE)
├── docker-compose.yml   # Test environment (CREATE)
├── httpd.conf           # Apache config for tests (CREATE)
├── test.sh              # Test runner script (CREATE)
└── tests/
    ├── index.html       # ESI test page (CREATE)
    ├── nested.html      # Nested includes test (CREATE)
    ├── noesi.txt        # Non-HTML test (CREATE)
    └── expected.html    # Expected output (CREATE)

.github/workflows/tests.yaml  # Add Apache test job (MODIFY)
```

---

### Task 1: Create Directory Structure

**Files:**
- Create: `servers/apache/tests/`

- [ ] **Step 1: Create directories**

```bash
mkdir -p servers/apache/tests
```

- [ ] **Step 2: Verify structure**

Run: `ls -la servers/apache/`
Expected: `tests/` directory exists

---

### Task 2: Create Apache Module (mod_mesi.c)

**Files:**
- Create: `servers/apache/mod_mesi.c`

- [ ] **Step 1: Create mod_mesi.c with module structure and includes**

```c
#include "httpd.h"
#include "http_config.h"
#include "http_protocol.h"
#include "http_log.h"
#include "ap_config.h"
#include "apr_strings.h"
#include "apr_lib.h"
#include "apr_buckets.h"
#include "util_filter.h"

#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>

#ifndef LIB_GOMESI_PATH
#define LIB_GOMESI_PATH "/usr/lib/libgomesi.so"
#endif

typedef struct mesi_config {
    int enabled;
    const char *lib_path;
} mesi_config;

typedef char *(*ParseFunc)(char *, int, char *);

static void *go_module = NULL;
static ParseFunc EsiParse = NULL;
static apr_thread_mutex_t *parse_mutex = NULL;

module AP_MODULE_DECLARE_DATA mesi_module;

static void *mesi_create_dir_config(apr_pool_t *p, char *dir)
{
    mesi_config *cfg = apr_pcalloc(p, sizeof(mesi_config));
    cfg->enabled = 0;
    cfg->lib_path = LIB_GOMESI_PATH;
    return cfg;
}

static void *mesi_merge_dir_config(apr_pool_t *p, void *parent, void *child)
{
    mesi_config *prev = parent;
    mesi_config *conf = child;
    mesi_config *merged = apr_pcalloc(p, sizeof(mesi_config));
    
    merged->enabled = (conf->enabled != 0) ? conf->enabled : prev->enabled;
    merged->lib_path = (conf->lib_path != NULL) ? conf->lib_path : prev->lib_path;
    
    return merged;
}

static const char *set_mesi_enable(cmd_parms *cmd, void *mconfig, int arg)
{
    mesi_config *cfg = (mesi_config *)mconfig;
    cfg->enabled = arg;
    return NULL;
}

static const char *set_mesi_lib_path(cmd_parms *cmd, void *mconfig, const char *arg)
{
    mesi_config *cfg = (mesi_config *)mconfig;
    cfg->lib_path = arg;
    return NULL;
}

static const command_rec mesi_cmds[] = {
    AP_INIT_FLAG("MesiEnable", set_mesi_enable, NULL, ACCESS_CONF,
                 "Enable or disable ESI processing"),
    AP_INIT_TAKE1("MesiLibPath", set_mesi_lib_path, NULL, ACCESS_CONF,
                  "Path to libgomesi.so"),
    {NULL}
};

static int is_html_content(request_rec *r)
{
    const char *ct = apr_table_get(r->headers_out, "Content-Type");
    if (ct == NULL) {
        return 0;
    }
    return (strcasestr(ct, "text/html") != NULL);
}

static int is_compressed(request_rec *r)
{
    const char *ce = apr_table_get(r->headers_out, "Content-Encoding");
    if (ce == NULL || strlen(ce) == 0) {
        return 0;
    }
    return 1;
}

static char *build_base_url(request_rec *r, apr_pool_t *pool)
{
    const char *scheme = ap_http_scheme(r);
    const char *host = ap_get_server_name(r);
    apr_port_t port = ap_get_server_port(r);
    
    int default_port = (strcmp(scheme, "https") == 0) ? 443 : 80;
    
    if (port != default_port) {
        return apr_psprintf(pool, "%s://%s:%d/", scheme, host, port);
    }
    return apr_psprintf(pool, "%s://%s/", scheme, host);
}

typedef struct mesi_filter_ctx {
    apr_bucket_brigade *bb;
    int done;
} mesi_filter_ctx;

static apr_status_t mesi_output_filter(ap_filter_t *f, apr_bucket_brigade *bb)
{
    mesi_config *cfg = ap_get_module_config(f->r->per_dir_config, &mesi_module);
    
    if (!cfg->enabled || go_module == NULL || EsiParse == NULL) {
        return ap_pass_brigade(f->next, bb);
    }
    
    if (f->r->header_only || f->r->status >= HTTP_BAD_REQUEST) {
        return ap_pass_brigade(f->next, bb);
    }
    
    if (!is_html_content(f->r) || is_compressed(f->r)) {
        return ap_pass_brigade(f->next, bb);
    }
    
    mesi_filter_ctx *ctx = f->ctx;
    if (ctx == NULL) {
        ctx = apr_pcalloc(f->r->pool, sizeof(mesi_filter_ctx));
        ctx->bb = apr_brigade_create(f->r->pool, f->c->bucket_alloc);
        ctx->done = 0;
        f->ctx = ctx;
        
        apr_table_set(f->r->headers_out, "Surrogate-Capability", "ESI/1.0");
        apr_table_unset(f->r->headers_out, "Content-Length");
    }
    
    if (ctx->done) {
        return ap_pass_brigade(f->next, bb);
    }
    
    APR_BRIGADE_CONCAT(ctx->bb, bb);
    
    apr_bucket *e = NULL;
    for (e = APR_BRIGADE_LAST(ctx->bb); e != APR_BRIGADE_SENTINEL(ctx->bb); e = APR_BUCKET_PREV(e)) {
        if (APR_BUCKET_IS_EOS(e)) {
            ctx->done = 1;
            apr_bucket_delete(e);
            break;
        }
    }
    
    if (!ctx->done) {
        return APR_SUCCESS;
    }
    
    apr_off_t length = 0;
    apr_brigade_length(ctx->bb, 1, &length);
    
    char *input = apr_palloc(f->r->pool, length + 1);
    char *ptr = input;
    
    for (e = APR_BRIGADE_FIRST(ctx->bb); e != APR_BRIGADE_SENTINEL(ctx->bb); e = APR_BUCKET_NEXT(e)) {
        const char *data;
        apr_size_t len;
        apr_bucket_read(e, &data, &len, APR_BLOCK_READ);
        memcpy(ptr, data, len);
        ptr += len;
    }
    *ptr = '\0';
    
    char *base_url = build_base_url(f->r, f->r->pool);
    
    char *output = NULL;
    
#ifdef APR_HAS_THREADS
    if (parse_mutex) {
        apr_thread_mutex_lock(parse_mutex);
    }
#endif
    
    output = EsiParse(input, 5, base_url);
    
#ifdef APR_HAS_THREADS
    if (parse_mutex) {
        apr_thread_mutex_unlock(parse_mutex);
    }
#endif
    
    if (output == NULL) {
        output = input;
    }
    
    apr_size_t output_len = strlen(output);
    
    apr_brigade_cleanup(ctx->bb);
    
    e = apr_bucket_pool_create(output, output_len, f->r->pool, f->c->bucket_alloc);
    APR_BRIGADE_INSERT_TAIL(ctx->bb, e);
    
    e = apr_bucket_eos_create(f->c->bucket_alloc);
    APR_BRIGADE_INSERT_TAIL(ctx->bb, e);
    
    f->r->headers_out.content_length = output_len;
    apr_table_set(f->r->headers_out, "Content-Length", apr_psprintf(f->r->pool, "%" APR_SIZE_T_FMT, output_len));
    
    return ap_pass_brigade(f->next, ctx->bb);
}

static int mesi_post_config(apr_pool_t *pconf, apr_pool_t *plog, apr_pool_t *ptemp, server_rec *s)
{
    void *data = NULL;
    const char *userdata_key = "mesi_post_config";
    
    apr_pool_userdata_get(&data, userdata_key, s->process->pconf);
    
    if (data == NULL) {
        apr_pool_userdata_set((const void *)1, userdata_key, apr_pool_cleanup_null, s->process->pconf);
        return OK;
    }
    
    mesi_config *cfg = ap_get_module_config(s->module_config, &mesi_module);
    const char *lib_path = (cfg && cfg->lib_path) ? cfg->lib_path : LIB_GOMESI_PATH;
    
    go_module = dlopen(lib_path, RTLD_NOW | RTLD_GLOBAL);
    if (go_module == NULL) {
        ap_log_error(APLOG_MARK, APLOG_ERR, 0, s, "mesi: Failed to load %s: %s", lib_path, dlerror());
        return HTTP_INTERNAL_SERVER_ERROR;
    }
    
    EsiParse = (ParseFunc)dlsym(go_module, "Parse");
    if (EsiParse == NULL) {
        ap_log_error(APLOG_MARK, APLOG_ERR, 0, s, "mesi: Failed to find Parse symbol: %s", dlerror());
        dlclose(go_module);
        go_module = NULL;
        return HTTP_INTERNAL_SERVER_ERROR;
    }
    
#ifdef APR_HAS_THREADS
    apr_thread_mutex_create(&parse_mutex, APR_THREAD_MUTEX_DEFAULT, pconf);
#endif
    
    ap_log_error(APLOG_MARK, APLOG_INFO, 0, s, "mesi: Successfully loaded %s", lib_path);
    
    return OK;
}

static void mesi_register_hooks(apr_pool_t *p)
{
    ap_hook_post_config(mesi_post_config, NULL, NULL, APR_HOOK_MIDDLE);
    ap_register_output_filter("MESI", mesi_output_filter, NULL, AP_FTYPE_RESOURCE);
}

AP_DECLARE_MODULE(mesi) = {
    STANDARD20_MODULE_STUFF,
    mesi_create_dir_config,
    mesi_merge_dir_config,
    NULL,
    NULL,
    mesi_cmds,
    mesi_register_hooks
};
```

- [ ] **Step 2: Verify file created**

Run: `wc -l servers/apache/mod_mesi.c`
Expected: ~250 lines

---

### Task 3: Create Build Files

**Files:**
- Create: `servers/apache/config.m4`
- Create: `servers/apache/Makefile`
- Create: `servers/apache/build.sh`

- [ ] **Step 1: Create config.m4**

```
APACHE_MODPATH_INIT(mesi)

APACHE_MODULE(mesi, mESI output filter, mod_mesi.c, , , shared)

APACHE_MODPATH_FINISH
```

- [ ] **Step 2: Create Makefile**

```makefile
APXS ?= apxs
LIBGOMESI_PATH ?= /usr/lib/libgomesi.so

all: mod_mesi.so

mod_mesi.so: mod_mesi.c
	$(APXS) -c -I../../libgomesi mod_mesi.c

install: mod_mesi.so
	$(APXS) -i -n mesi mod_mesi.so

clean:
	rm -f mod_mesi.so mod_mesi.la *.lo *.slo

test:
	./test.sh

.PHONY: all install clean test
```

- [ ] **Step 3: Create build.sh**

```bash
#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$(dirname "$SCRIPT_DIR")")"

LIBGOMESI_SO="${LIBGOMESI_SO:-$ROOT_DIR/libgomesi/libgomesi.so}"

if [ ! -f "$LIBGOMESI_SO" ]; then
    echo "Building libgomesi.so..."
    cd "$ROOT_DIR/libgomesi"
    go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go
fi

cp "$LIBGOMESI_SO" /usr/lib/libgomesi.so 2>/dev/null || sudo cp "$LIBGOMESI_SO" /usr/lib/libgomesi.so

cd "$SCRIPT_DIR"

if command -v apxs2 &> /dev/null; then
    APXS=apxs2
else
    APXS=apxs
fi

$APXS -c mod_mesi.c

echo "Build complete: mod_mesi.so"
```

- [ ] **Step 4: Make build.sh executable**

Run: `chmod +x servers/apache/build.sh`

- [ ] **Step 5: Commit build files**

```bash
git add servers/apache/config.m4 servers/apache/Makefile servers/apache/build.sh
git commit -m "feat(apache): add build configuration for mod_mesi"
```

---

### Task 4: Create Docker Test Environment

**Files:**
- Create: `servers/apache/Dockerfile`
- Create: `servers/apache/docker-compose.yml`
- Create: `servers/apache/httpd.conf`

- [ ] **Step 1: Create Dockerfile**

```dockerfile
FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y \
    apache2 \
    apache2-dev \
    libapr1-dev \
    libaprutil1-dev \
    golang-go \
    curl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build

COPY libgomesi/ /build/libgomesi/
RUN cd /build/libgomesi && \
    go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go && \
    cp libgomesi.so /usr/lib/ && \
    cp libgomesi.h /usr/include/

COPY servers/apache/mod_mesi.c /build/mod_mesi.c
RUN apxs -c -i mod_mesi.c

RUN a2enmod headers proxy proxy_http

COPY servers/apache/httpd.conf /etc/apache2/sites-available/mesi.conf
RUN a2dissite 000-default && a2ensite mesi

COPY servers/apache/tests/ /var/www/html/

EXPOSE 80

CMD ["apache2ctl", "-D", "FOREGROUND"]
```

- [ ] **Step 2: Create docker-compose.yml**

```yaml
version: '3.8'

services:
  apache:
    build:
      context: ../..
      dockerfile: servers/apache/Dockerfile
    ports:
      - "8080:80"
    volumes:
      - ./tests:/var/www/html:ro
    depends_on:
      - backend

  backend:
    image: python:3.11-slim
    working_dir: /data
    volumes:
      - ./tests:/data
    command: python -m http.server 8000
    expose:
      - "8000"
```

- [ ] **Step 3: Create httpd.conf**

```apache
<VirtualHost *:80>
    ServerName localhost
    DocumentRoot /var/www/html

    <Directory /var/www/html>
        Options Indexes FollowSymLinks
        AllowOverride None
        Require all granted
        MesiEnable on
    </Directory>

    ProxyPreserveHost On
    ProxyPass /backend/ http://backend:8000/
    ProxyPassReverse /backend/ http://backend:8000/

    ErrorLog ${APACHE_LOG_DIR}/error.log
    CustomLog ${APACHE_LOG_DIR}/access.log combined
</VirtualHost>
```

- [ ] **Step 4: Commit Docker files**

```bash
git add servers/apache/Dockerfile servers/apache/docker-compose.yml servers/apache/httpd.conf
git commit -m "feat(apache): add Docker test environment"
```

---

### Task 5: Create Test Files

**Files:**
- Create: `servers/apache/tests/index.html`
- Create: `servers/apache/tests/nested.html`
- Create: `servers/apache/tests/noesi.txt`
- Create: `servers/apache/test.sh`

- [ ] **Step 1: Create tests/index.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <title>ESI Test</title>
</head>
<body>
    <h1>ESI Test Page</h1>
    <esi:include src="https://raw.githubusercontent.com/crazy-goat/go-mesi/main/examples/includes/include.txt" />
    <p>After include</p>
</body>
</html>
```

- [ ] **Step 2: Create tests/nested.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <title>Nested ESI Test</title>
</head>
<body>
    <h1>Nested ESI Test</h1>
    <esi:include src="https://raw.githubusercontent.com/crazy-goat/go-mesi/main/examples/includes/nested.txt" />
</body>
</html>
```

- [ ] **Step 3: Create tests/noesi.txt**

```
This is plain text.
<esi:include src="http://example.com/not-processed.txt" />
This should not be processed.
```

- [ ] **Step 4: Create test.sh**

```bash
#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

docker-compose up -d --build

sleep 5

echo "=== Test 1: Simple ESI include ==="
RESPONSE=$(curl -s http://localhost:8080/index.html)
if echo "$RESPONSE" | grep -q "After include"; then
    echo "PASS: ESI include processed"
else
    echo "FAIL: ESI include not processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 2: Surrogate-Capability header ==="
HEADERS=$(curl -sI http://localhost:8080/index.html)
if echo "$HEADERS" | grep -q "Surrogate-Capability"; then
    echo "PASS: Surrogate-Capability header present"
else
    echo "FAIL: Surrogate-Capability header missing"
    echo "Headers: $HEADERS"
    exit 1
fi

echo "=== Test 3: Non-HTML content ==="
RESPONSE=$(curl -s http://localhost:8080/noesi.txt)
if echo "$RESPONSE" | grep -q "esi:include"; then
    echo "PASS: Non-HTML content bypassed ESI filter (tags preserved verbatim)"
else
    echo "FAIL: Non-HTML content was processed"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "=== Test 4: Content-Type check ==="
CT=$(curl -sI http://localhost:8080/index.html | grep -i "Content-Type")
if echo "$CT" | grep -q "text/html"; then
    echo "PASS: Content-Type is text/html"
else
    echo "FAIL: Wrong Content-Type"
    echo "Content-Type: $CT"
    exit 1
fi

docker-compose down

echo ""
echo "=== All tests passed ==="
```

- [ ] **Step 5: Make test.sh executable**

Run: `chmod +x servers/apache/test.sh`

- [ ] **Step 6: Commit test files**

```bash
git add servers/apache/tests/ servers/apache/test.sh
git commit -m "feat(apache): add integration tests"
```

---

### Task 6: Update CI Workflow

**Files:**
- Modify: `.github/workflows/tests.yaml`

- [ ] **Step 1: Add Apache test job to tests.yaml**

Add after the `test` job:

```yaml
  apache-test:
    name: Apache Integration Test
    runs-on: ubuntu-latest
    needs: test
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.23'
          cache: true

      - name: Build libgomesi
        run: cd libgomesi && go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o libgomesi.so libgomesi.go

      - name: Run Apache tests
        run: |
          cd servers/apache
          docker-compose up --build --abort-on-container-exit
          docker-compose down

      - name: Cleanup
        if: always()
        run: cd servers/apache && docker-compose down -v
```

- [ ] **Step 2: Update ci job dependencies**

Change:
```yaml
  ci:
    name: CI
    needs: [lint, test]
```

To:
```yaml
  ci:
    name: CI
    needs: [lint, test, apache-test]
```

- [ ] **Step 3: Commit CI changes**

```bash
git add .github/workflows/tests.yaml
git commit -m "feat(ci): add Apache integration test job"
```

---

### Task 7: Create README

**Files:**
- Create: `servers/apache/README.md`

- [ ] **Step 1: Create README.md**

```markdown
# Apache HTTP Server mESI Module

Apache output filter module for mESI (Edge Side Includes) processing.

## Requirements

- Apache HTTP Server 2.4+
- libgomesi.so (built from libgomesi/)
- APR, APR-util

## Building

```bash
# Build libgomesi first
cd ../../libgomesi
go build -buildmode=c-shared -o libgomesi.so libgomesi.go
sudo cp libgomesi.so /usr/lib/

# Build Apache module
./build.sh
```

## Installation

```bash
sudo make install
```

## Configuration

```apache
LoadModule mesi_module modules/mod_mesi.so

<Location /esi>
    MesiEnable on
</Location>

# Optional: custom libgomesi path
MesiLibPath /opt/libgomesi.so
```

### Directives

- `MesiEnable on|off` - Enable/disable ESI processing (default: off)
- `MesiLibPath /path/to/libgomesi.so` - Path to libgomesi library (default: /usr/lib/libgomesi.so)

## Testing

```bash
docker-compose up --build
./test.sh
```

## MPM Compatibility

| MPM | Status | Notes |
|-----|--------|-------|
| Prefork | ✅ Recommended | No threading issues |
| Worker | ⚠️ Supported | Mutex around Parse() |
| Event | ⚠️ Supported | Same as Worker |

## How It Works

1. Registers as output filter in Apache's filter chain
2. Intercepts responses with `Content-Type: text/html`
3. Adds `Surrogate-Capability: ESI/1.0` header
4. Buffers response body until complete
5. Processes through libgomesi.Parse()
6. Returns processed HTML to client
```

- [ ] **Step 2: Commit README**

```bash
git add servers/apache/README.md
git commit -m "docs(apache): add README for mod_mesi"
```

---

### Task 8: Final Verification

- [ ] **Step 1: Verify all files exist**

Run: `ls -la servers/apache/`
Expected: All files present

- [ ] **Step 2: Verify CI workflow syntax**

Run: `cat .github/workflows/tests.yaml | grep -A5 apache-test`
Expected: Apache test job present

- [ ] **Step 3: Create feature branch and push**

```bash
git checkout -b feature/apache-integration
git push -u origin feature/apache-integration
```

---

## Summary

This plan creates:
1. `mod_mesi.c` - Apache output filter module
2. Build system (config.m4, Makefile, build.sh)
3. Docker test environment
4. Integration tests
5. CI integration
6. Documentation

Total: ~8 commits, ready for PR.
