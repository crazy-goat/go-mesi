# Fix #106: AllowedHosts Port Bug + Private-IP Bypass Opt-In

## Problem

Two bugs in `AllowedHosts` logic in `mesi/fetchUrl.go`:

### 1. Port Bug

`parsedURL.Host` includes port (e.g., `example.com:8080`), so it doesn't match `example.com` in `AllowedHosts`:

```go
parsed.Host       = "example.com:8080"
AllowedHosts[0]   = "example.com"
// neither equality nor suffix match → false negative
```

Users with allowlist see their allowed hosts silently rejected when URLs contain ports.

### 2. No Opt-In for Internal Proxy Setups

Some deployments legitimately need to include from internal services (e.g., reverse proxy to `127.0.0.1:8080`). Currently there's no way to allow this while keeping `BlockPrivateIPs=true` for other hosts.

## Solution

### 1. Port Fix

Use `parsedURL.Hostname()` instead of `parsedURL.Host` to strip port before comparison.

### 2. Opt-In Flag

Add `AllowPrivateIPsForAllowedHosts bool` field to `EsiParserConfig`. When `true`, hosts in `AllowedHosts` bypass the private-IP check.

## Design

### 1. New Config Field (parser.go)

```go
type EsiParserConfig struct {
    // ... existing fields ...

    // AllowPrivateIPsForAllowedHosts allows hosts in AllowedHosts to bypass
    // the BlockPrivateIPs check.
    //
    // SECURITY WARNING: This creates a potential SSRF vector if an attacker
    // can control DNS for a host in AllowedHosts. Only use in trusted environments
    // where you control DNS resolution (e.g., internal reverse proxy setups).
    //
    // When true, requests to hosts in AllowedHosts will NOT be checked for
    // private/reserved IP addresses at dial time.
    //
    // Default: false (private IPs always blocked regardless of AllowedHosts).
    AllowPrivateIPsForAllowedHosts bool
}
```

### 2. Port Fix in isURLSafe (fetchUrl.go)

```go
func isURLSafe(requestedURL string, config EsiParserConfig) error {
    parsedURL, err := url.Parse(requestedURL)
    if err != nil {
        return errors.New("invalid url: " + err.Error())
    }

    host := parsedURL.Hostname() // CHANGED: was parsedURL.Host

    // Relative URLs have no host and no scheme
    if parsedURL.Scheme == "" && host == "" {
        return nil
    }

    if host == "" {
        return errors.New("url has no host")
    }

    if len(config.AllowedHosts) > 0 {
        allowed := false
        for _, allowedHost := range config.AllowedHosts {
            if host == allowedHost || strings.HasSuffix(host, "."+allowedHost) {
                allowed = true
                break
            }
        }
        if !allowed {
            return errors.New("host not in allowed list: " + host)
        }
    }

    return nil
}
```

### 3. Helper Function (fetchUrl.go)

```go
// hostInAllowedHosts checks if a hostname matches any entry in AllowedHosts.
// Matches exact hostnames and subdomains (e.g., "api.example.com" matches "example.com").
func hostInAllowedHosts(host string, config EsiParserConfig) bool {
    for _, allowed := range config.AllowedHosts {
        if host == allowed || strings.HasSuffix(host, "."+allowed) {
            return true
        }
    }
    return false
}
```

### 4. Modified Client Selection (fetchUrl.go)

```go
func singleFetchUrlWithContext(requestedURL string, config EsiParserConfig, ctx context.Context) (string, bool, error) {
    // ... existing code up to isURLSafe check ...

    if err := isURLSafe(requestedURL, config); err != nil {
        logger.Debug("fetch_ssrf_error", "url", requestedURL, "error", err.Error())
        return "", false, errors.New("ssrf validation failed: " + err.Error())
    }

    parsed, _ := url.Parse(requestedURL)
    
    var client httpDoer
    if config.HTTPClient != nil {
        client = config.HTTPClient
    } else if config.AllowPrivateIPsForAllowedHosts && hostInAllowedHosts(parsed.Hostname(), config) {
        // Allowed host with private-IP bypass — use standard client
        client = &http.Client{Timeout: config.Timeout}
    } else {
        // Use SSRF-safe transport
        client = &http.Client{
            Timeout:   config.Timeout,
            Transport: NewSSRFSafeTransport(config),
        }
    }

    // ... rest of existing code ...
}
```

### 5. Update Godoc for AllowedHosts (parser.go)

```go
// AllowedHosts restricts ESI includes to specified domains.
// Empty list allows all hosts (subject to BlockPrivateIPs).
//
// Examples:
//   - ["example.com"] — allows example.com and *.example.com
//   - ["internal.local", "api.trusted.com"] — allows multiple domains
//
// Note: AllowedHosts does NOT bypass BlockPrivateIPs by default.
// Use AllowPrivateIPsForAllowedHosts to enable private-IP bypass for allowed hosts.
AllowedHosts []string
```

## Testing Plan

### Unit Tests

1. **Port handling tests** (extend `TestIsURLSafe_AllowedHosts`):
   ```go
   {"allowed host with port", "http://example.com:8080/test", []string{"example.com"}, false},
   {"allowed subdomain with port", "http://api.example.com:443/test", []string{"example.com"}, false},
   {"port in allowed list", "http://example.com:8080/test", []string{"example.com:8080"}, true}, // should fail
   ```

2. **AllowPrivateIPsForAllowedHosts tests**:
   ```go
   func TestAllowPrivateIPsForAllowedHosts(t *testing.T) {
       // Test that allowed host with private IP is blocked when flag is false
       // Test that allowed host with private IP is allowed when flag is true
   }
   ```

3. **Integration test**:
   - Start test server on 127.0.0.1
   - Configure `AllowedHosts: ["localhost"]`
   - Verify request blocked with `AllowPrivateIPsForAllowedHosts=false`
   - Verify request allowed with `AllowPrivateIPsForAllowedHosts=true`

## Files to Modify

1. `mesi/parser.go` — Add `AllowPrivateIPsForAllowedHosts` field, update godoc
2. `mesi/fetchUrl.go` — Port fix, add `hostInAllowedHosts`, modify client selection
3. `mesi/fetchUrl_test.go` — Add tests for port handling and opt-in flag
4. `README.md` — Document new config option

## Acceptance Criteria

- [ ] `parsedURL.Hostname()` used instead of `parsedURL.Host`
- [ ] `AllowPrivateIPsForAllowedHosts` field added to `EsiParserConfig`
- [ ] Hosts in `AllowedHosts` bypass private-IP check when flag is `true`
- [ ] Default behavior unchanged (private IPs blocked regardless of `AllowedHosts`)
- [ ] Tests cover port handling and opt-in flag behavior
- [ ] Godoc updated for `AllowedHosts` and new field
- [ ] README updated with new config option

## Security Considerations

- **Default safe**: `AllowPrivateIPsForAllowedHosts=false` by default
- **Explicit opt-in**: Users must consciously enable the bypass
- **Documented risk**: Godoc clearly warns about SSRF vector
- **Dial-time protection preserved**: Non-allowed hosts still protected by `safeDialer`
