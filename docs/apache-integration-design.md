# Apache HTTP Server (httpd) Integration Design

## Summary

Add Apache HTTP Server module (`mod_mesi`) to enable mESI processing, similar to existing Nginx integration. Uses output filter approach with libgomesi.so shared library.

## Architecture

```
servers/apache/
├── mod_mesi.c           # Apache output filter module
├── config.m4            # apxs build config
├── Makefile             # Build targets
├── build.sh             # Build script
├── Dockerfile           # Test container
├── docker-compose.yml   # Test environment
├── httpd.conf           # Apache config for tests
└── tests/
    ├── index.html       # ESI test page
    └── expected.html    # Expected output
```

## Data Flow

```
Request → Apache → Backend (proxy/static)
                    ↓
            Response headers
                    ↓
         mod_mesi head_filter:
         - Check Content-Type: text/html
         - Check no compression
         - Add Surrogate-Capability header
                    ↓
            Response body chunks
                    ↓
         mod_mesi body_filter:
         - Accumulate chunks
         - On last_buf: call libgomesi.Parse()
         - Return processed HTML
                    ↓
              Client
```

Base URL construction: `scheme://hostname/` from `r->parsed_uri` + `r->hostname`

## MPM Compatibility

| MPM | Thread Safety | Solution |
|-----|---------------|----------|
| **Prefork** | No threads | Safe - each process has own instance |
| **Worker** | Threads per process | Go runtime requires `GOMAXPROCS=1` per process or mutex |
| **Event** | Async + threads | Same as Worker |

**Implementation:**
- `dlopen` in `ap_hook_post_config` (once per process)
- Mutex around `Parse()` for Worker/Event MPM
- Documentation: recommend Prefork for stability

## Configuration

```apache
# httpd.conf / .htaccess
LoadModule mesi_module modules/mod_mesi.so

<Location /esi>
    MesiEnable on
</Location>

# Or globally
MesiEnable on
```

**Directives:**
- `MesiEnable on|off` - enable/disable ESI processing
- `MesiLibPath /path/to/libgomesi.so` - optional library path

**Defaults:**
- `MesiEnable off`
- `MesiLibPath /usr/lib/libgomesi.so`

## Testing

### Integration Tests (Docker)

```yaml
# docker-compose.yml
services:
  apache:
    build: .
    ports: ["8080:80"]
    volumes: [./tests:/var/www/html]
  backend:
    image: python:3.11
    command: python -m http.server 8000
```

### Test Scenarios

1. Simple `<esi:include>` - verify include executed
2. Nested includes - max depth handling
3. Invalid URL - verify fallback content
4. Non-HTML content - verify no processing
5. GZIP content - verify skipped

### CI Integration

```yaml
# .github/workflows/tests.yaml
apache-test:
  name: Apache Integration Test
  runs-on: ubuntu-latest
  needs: test
  steps:
    - uses: actions/checkout@v4
    - name: Build libgomesi
      run: cd libgomesi && go build -buildmode=c-shared -o libgomesi.so
    - name: Build Apache module
      run: cd servers/apache && ./build.sh
    - name: Run tests
      run: cd servers/apache && docker-compose up --abort-on-container-exit
```

## Implementation Notes

### mod_mesi.c Structure

```c
// Module declaration
module AP_MODULE_DECLARE_DATA mesi_module;

// Configuration
typedef struct {
    int enabled;
    const char *lib_path;
} mesi_config;

// Filter functions
static apr_status_t mesi_output_filter(ap_filter_t *f, apr_bucket_brigade *bb);

// Hooks
static void mesi_register_hooks(apr_pool_t *p);
static int mesi_post_config(apr_pool_t *pconf, apr_pool_t *plog, apr_pool_t *ptemp, server_rec *s);

// libgomesi interface
typedef char* (*ParseFunc)(char*, int, char*);
static void *go_module = NULL;
static ParseFunc EsiParse = NULL;
```

### Key Functions

1. `mesi_post_config` - load libgomesi.so via dlopen
2. `mesi_output_filter` - process HTML content
3. `mesi_create_dir_config` - create per-directory config
4. `mesi_merge_dir_config` - merge config hierarchy

### Error Handling

- dlopen failure: log error, return DECLINED
- Parse failure: return original content
- Memory allocation: return HTTP_INTERNAL_SERVER_ERROR

## Dependencies

- Apache httpd >= 2.4
- libgomesi.so (built from libgomesi/)
- apr, apr-util

## References

- Nginx module: `servers/nginx/ngx_http_mesi_module.c`
- libgomesi bridge: `libgomesi/`
- Apache module docs: https://httpd.apache.org/docs/2.4/developer/
