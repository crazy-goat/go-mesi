# SSRF Fix for Issue #92: Host Header Trusted as Base URL

## Problem

`build_base_url` in `servers/apache/mod_mesi.c:67-78` constructs the `defaultUrl` passed to `libgomesi.Parse` using `ap_get_server_name(r)`, which returns the client-supplied `Host:` header when `UseCanonicalName` is `Off` (Apache's default).

This allows attackers to:
1. Redirect relative ESI includes to attacker-controlled servers via Host header injection
2. Perform SSRF attacks by injecting `<esi:include src="http://169.254.169.254/...">`
3. Exfiltrate cloud metadata or access internal services

## Solution Design

### 1. Fix build_base_url in mod_mesi.c

**Current code (line 67-78):**
```c
static char *build_base_url(request_rec *r, apr_pool_t *pool) {
    const char *scheme = ap_http_scheme(r);
    const char *host = ap_get_server_name(r);   // <-- client-controlled
    apr_port_t port = ap_get_server_port(r);
    // ...
}
```

**Fixed code:**
```c
static char *build_base_url(request_rec *r, apr_pool_t *pool) {
    const char *scheme = ap_http_scheme(r);
    const char *host = r->server->server_hostname
                         ? r->server->server_hostname
                         : ap_get_server_name(r);   // fallback only
    apr_port_t port = ap_get_server_port(r);
    // ...
}
```

This ensures we use the configured server name (`ServerName` directive) instead of the client-supplied Host header.

### 2. Add MesiAllowedHosts Directive

Add a new Apache directive to whitelist allowed hostnames for ESI includes:

```apache
MesiAllowedHosts backend.internal static.example.com
```

**Implementation:**
- Add to `mesi_cmds` array in mod_mesi.c
- Parse space-separated hostnames into `mesi_config->allowed_hosts` array
- Pass to libgomesi via new `ParseWithConfig` export

### 3. Add MesiBlockPrivateIPs Directive

Expose the Go library's `BlockPrivateIPs` option:

```apache
MesiBlockPrivateIPs On   # default: On
```

**Implementation:**
- Add to `mesi_cmds` array
- Store in `mesi_config->block_private_ips` (int, default: 1)
- Pass to libgomesi

### 4. Extend libgomesi.go with ParseWithConfig

Add new export to pass full configuration:

```go
//export ParseWithConfig
func ParseWithConfig(input *C.char, maxDepth C.int, defaultUrl *C.char, 
                     allowedHosts *C.char, blockPrivateIPs C.int) *C.char {
    goInput := C.GoString(input)
    goMaxDepth := int(maxDepth)
    goDefaultUrl := C.GoString(defaultUrl)
    
    // Parse allowedHosts (comma or space separated)
    hostsStr := C.GoString(allowedHosts)
    var hosts []string
    if hostsStr != "" {
        // Split by comma or space
        for _, h := range strings.FieldsFunc(hostsStr, func(r rune) bool {
            return r == ',' || r == ' '
        }) {
            if h != "" {
                hosts = append(hosts, h)
            }
        }
    }
    
    config := mesi.EsiParserConfig{
        DefaultUrl:      goDefaultUrl,
        MaxDepth:        uint(goMaxDepth),
        Timeout:         30 * time.Second,
        AllowedHosts:    hosts,
        BlockPrivateIPs: blockPrivateIPs != 0,
    }
    result := mesi.MESIParse(goInput, config)
    return C.CString(result)
}
```

### 5. Update mod_mesi.c to Use ParseWithConfig

Modify `mesi_response_filter` to:
1. Build the allowedHosts string from config
2. Call `ParseWithConfig` instead of `Parse`

## Testing Plan

### Unit Tests (C module)
1. Test `build_base_url` uses `server_hostname` not Host header
2. Test `MesiAllowedHosts` directive parsing
3. Test `MesiBlockPrivateIPs` directive parsing

### Integration Tests
1. **Host header injection test**: Send `Host: evil.example.com`, verify ESI includes use canonical host
2. **AllowedHosts test**: Configure `MesiAllowedHosts backend.internal`, verify includes to other hosts are rejected
3. **BlockPrivateIPs test**: Configure `MesiBlockPrivateIPs On`, verify requests to 127.0.0.1/169.254.x.x are blocked
4. **Backward compatibility**: Verify existing `Parse` export still works

## Files to Modify

1. `servers/apache/mod_mesi.c` - Fix build_base_url, add directives, update filter
2. `libgomesi/libgomesi.go` - Add ParseWithConfig export
3. `servers/apache/tests/` - Add unit tests (if test framework exists)

## Success Criteria

1. Host header injection no longer affects ESI base URL
2. MesiAllowedHosts directive works correctly
3. MesiBlockPrivateIPs directive works correctly
4. All existing tests pass
5. New tests cover the SSRF fix scenarios
