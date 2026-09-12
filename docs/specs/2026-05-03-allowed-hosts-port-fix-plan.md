# AllowedHosts Port Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix port bug in AllowedHosts matching and add opt-in flag to bypass private-IP check for allowed hosts.

**Architecture:** Use `parsedURL.Hostname()` instead of `parsedURL.Host` to strip port. Add `AllowPrivateIPsForAllowedHosts` config field. When enabled, hosts in AllowedHosts use standard HTTP client without SSRF-safe transport.

**Tech Stack:** Go 1.x, net/url, net/http

---

## File Structure

| File | Action | Purpose |
|------|--------|---------|
| `mesi/parser.go` | Modify | Add `AllowPrivateIPsForAllowedHosts` field to `EsiParserConfig` |
| `mesi/fetchUrl.go` | Modify | Port fix in `isURLSafe`, add `hostInAllowedHosts` helper, modify client selection in `singleFetchUrlWithContext` |
| `mesi/fetchUrl_test.go` | Modify | Add port handling tests, add opt-in flag tests |
| `README.md` | Modify | Document new config option |

---

### Task 1: Add Config Field

**Files:**
- Modify: `mesi/parser.go:17-34`

- [ ] **Step 1: Add `AllowPrivateIPsForAllowedHosts` field to `EsiParserConfig`**

```go
type EsiParserConfig struct {
	Context               context.Context
	DefaultUrl            string
	MaxDepth              uint
	Timeout               time.Duration
	ParseOnHeader         bool
	AllowedHosts          []string
	BlockPrivateIPs       bool
	AllowPrivateIPsForAllowedHosts bool // NEW: allows AllowedHosts to bypass private-IP check
	MaxResponseSize       int64
	MaxConcurrentRequests int
	HTTPClient            *http.Client
	Cache                 Cache
	CacheTTL              time.Duration
	CacheKeyFunc          CacheKeyFunc
	Debug                 bool
	Logger                Logger
	requestSemaphore      chan struct{}
}
```

- [ ] **Step 2: Run tests to verify no breakage**

Run: `go test ./mesi/... -v -count=1`
Expected: All tests pass

- [ ] **Step 3: Commit**

```bash
git add mesi/parser.go
git commit -m "feat(mesi): add AllowPrivateIPsForAllowedHosts config field"
```

---

### Task 2: Write Port Handling Tests

**Files:**
- Modify: `mesi/fetchUrl_test.go:640-672`

- [ ] **Step 1: Add port handling test cases to `TestIsURLSafe_AllowedHosts`**

```go
func TestIsURLSafe_AllowedHosts(t *testing.T) {
	tests := []struct {
		name         string
		url          string
		allowedHosts []string
		wantErr      bool
	}{
		{"allowed exact", "http://example.com/test", []string{"example.com"}, false},
		{"allowed subdomain", "http://api.example.com/test", []string{"example.com"}, false},
		{"not allowed", "http://other.com/test", []string{"example.com"}, true},
		{"multiple allowed", "http://foo.com/test", []string{"example.com", "foo.com"}, false},
		{"empty allowed list", "http://example.com/test", []string{}, false},
		// NEW: Port handling tests
		{"allowed host with port", "http://example.com:8080/test", []string{"example.com"}, false},
		{"allowed subdomain with port", "http://api.example.com:443/test", []string{"example.com"}, false},
		{"not allowed with port", "http://other.com:8080/test", []string{"example.com"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{
				BlockPrivateIPs: true,
				AllowedHosts:    tt.allowedHosts,
			}
			err := isURLSafe(tt.url, config)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./mesi/... -run TestIsURLSafe_AllowedHosts -v`
Expected: FAIL - "allowed host with port" and "allowed subdomain with port" should fail

- [ ] **Step 3: Commit**

```bash
git add mesi/fetchUrl_test.go
git commit -m "test(mesi): add port handling test cases for AllowedHosts"
```

---

### Task 3: Fix Port Bug in isURLSafe

**Files:**
- Modify: `mesi/fetchUrl.go:41-73`

- [ ] **Step 1: Change `parsedURL.Host` to `parsedURL.Hostname()` in `isURLSafe`**

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

- [ ] **Step 2: Run tests to verify port tests pass**

Run: `go test ./mesi/... -run TestIsURLSafe_AllowedHosts -v`
Expected: PASS

- [ ] **Step 3: Run all tests**

Run: `go test ./mesi/... -v -count=1`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add mesi/fetchUrl.go
git commit -m "fix(mesi): use Hostname() instead of Host in AllowedHosts check"
```

---

### Task 4: Add hostInAllowedHosts Helper Function

**Files:**
- Modify: `mesi/fetchUrl.go` (add after `isPrivateOrReservedIP` function)

- [ ] **Step 1: Add `hostInAllowedHosts` helper function**

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

- [ ] **Step 2: Run tests to verify no breakage**

Run: `go test ./mesi/... -v -count=1`
Expected: All tests pass

- [ ] **Step 3: Commit**

```bash
git add mesi/fetchUrl.go
git commit -m "feat(mesi): add hostInAllowedHosts helper function"
```

---

### Task 5: Write AllowPrivateIPsForAllowedHosts Tests

**Files:**
- Modify: `mesi/fetchUrl_test.go` (add after `TestIsURLSafe_AllowedHosts`)

- [ ] **Step 1: Add test for `AllowPrivateIPsForAllowedHosts` flag**

```go
func TestAllowPrivateIPsForAllowedHosts(t *testing.T) {
	// Start a test server on 127.0.0.1 (private IP)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	}))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)
	serverHost := serverURL.Hostname()

	t.Run("blocked when flag is false", func(t *testing.T) {
		config := EsiParserConfig{
			BlockPrivateIPs:                true,
			AllowPrivateIPsForAllowedHosts: false,
			AllowedHosts:                   []string{serverHost},
			Timeout:                        5 * time.Second,
		}

		_, _, err := singleFetchUrlWithContext(server.URL, config, context.Background())
		if err == nil {
			t.Error("expected error for private IP when flag is false")
		}
	})

	t.Run("allowed when flag is true", func(t *testing.T) {
		config := EsiParserConfig{
			BlockPrivateIPs:                true,
			AllowPrivateIPsForAllowedHosts: true,
			AllowedHosts:                   []string{serverHost},
			Timeout:                        5 * time.Second,
		}

		result, _, err := singleFetchUrlWithContext(server.URL, config, context.Background())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if result != "test response" {
			t.Errorf("expected 'test response', got: %q", result)
		}
	})

	t.Run("blocked when host not in AllowedHosts", func(t *testing.T) {
		config := EsiParserConfig{
			BlockPrivateIPs:                true,
			AllowPrivateIPsForAllowedHosts: true,
			AllowedHosts:                   []string{"other.example.com"},
			Timeout:                        5 * time.Second,
		}

		_, _, err := singleFetchUrlWithContext(server.URL, config, context.Background())
		if err == nil {
			t.Error("expected error for host not in AllowedHosts")
		}
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./mesi/... -run TestAllowPrivateIPsForAllowedHosts -v`
Expected: FAIL

- [ ] **Step 3: Commit**

```bash
git add mesi/fetchUrl_test.go
git commit -m "test(mesi): add tests for AllowPrivateIPsForAllowedHosts flag"
```

---

### Task 6: Implement AllowPrivateIPsForAllowedHosts in Client Selection

**Files:**
- Modify: `mesi/fetchUrl.go:181-195`

- [ ] **Step 1: Modify client selection logic in `singleFetchUrlWithContext`**

Replace the client creation section with:

```go
	var client httpDoer
	if config.HTTPClient != nil {
		client = config.HTTPClient
	} else if config.AllowPrivateIPsForAllowedHosts && hostInAllowedHosts(parsed.Hostname(), config) {
		// Allowed host with private-IP bypass opt-in - use standard client
		client = &http.Client{Timeout: config.Timeout}
	} else {
		// Use SSRF-safe transport with dial-time private IP blocking
		transport := NewSSRFSafeTransport(config)
		client = &http.Client{
			Timeout:   config.Timeout,
			Transport: transport,
		}
	}
```

- [ ] **Step 2: Run tests to verify flag tests pass**

Run: `go test ./mesi/... -run TestAllowPrivateIPsForAllowedHosts -v`
Expected: PASS

- [ ] **Step 3: Run all tests**

Run: `go test ./mesi/... -v -count=1`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add mesi/fetchUrl.go
git commit -m "feat(mesi): implement AllowPrivateIPsForAllowedHosts bypass"
```

---

### Task 7: Update Godoc

**Files:**
- Modify: `mesi/parser.go:17-34`

- [ ] **Step 1: Add documentation comments**

```go
type EsiParserConfig struct {
	Context               context.Context
	DefaultUrl            string
	MaxDepth              uint
	Timeout               time.Duration
	ParseOnHeader         bool
	// AllowedHosts restricts ESI includes to specified domains.
	// Empty list allows all hosts (subject to BlockPrivateIPs).
	//
	// Note: AllowedHosts does NOT bypass BlockPrivateIPs by default.
	// Use AllowPrivateIPsForAllowedHosts to enable private-IP bypass.
	AllowedHosts []string
	BlockPrivateIPs bool
	// AllowPrivateIPsForAllowedHosts allows hosts in AllowedHosts to bypass
	// the BlockPrivateIPs check.
	//
	// SECURITY WARNING: This creates a potential SSRF vector if an attacker
	// can control DNS for a host in AllowedHosts. Only use in trusted environments.
	//
	// Default: false (private IPs always blocked regardless of AllowedHosts).
	AllowPrivateIPsForAllowedHosts bool
	MaxResponseSize       int64
	MaxConcurrentRequests int
	HTTPClient            *http.Client
	Cache                 Cache
	CacheTTL              time.Duration
	CacheKeyFunc          CacheKeyFunc
	Debug                 bool
	Logger                Logger
	requestSemaphore      chan struct{}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./mesi/... -v -count=1`
Expected: All tests pass

- [ ] **Step 3: Commit**

```bash
git add mesi/parser.go
git commit -m "docs(mesi): add godoc for AllowedHosts and AllowPrivateIPsForAllowedHosts"
```

---

### Task 8: Update README

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add documentation for `AllowPrivateIPsForAllowedHosts`**

Add after the AllowedHosts section:

```markdown
**AllowPrivateIPsForAllowedHosts**

When set to `true`, hosts in `AllowedHosts` are allowed to resolve to private/reserved IP addresses, bypassing the `BlockPrivateIPs` check.

Security Warning: This creates a potential SSRF vector if an attacker can control DNS for a host in AllowedHosts. Only use in trusted environments where you control DNS resolution.

Use case: Internal reverse proxy setups where ESI includes need to fetch from internal services.

Default: `false`

Example:
config := mesi.EsiParserConfig{
    BlockPrivateIPs:                true,
    AllowedHosts:                   []string{"internal.local"},
    AllowPrivateIPsForAllowedHosts: true,
}
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: document AllowPrivateIPsForAllowedHosts config option"
```

---

### Task 9: Final Verification

- [ ] **Step 1: Run all tests**

Run: `go test ./... -v -count=1`
Expected: All tests pass

- [ ] **Step 2: Run linter**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 3: Verify godoc**

Run: `go doc -all ./mesi/ | grep -A 10 "AllowPrivateIPsForAllowedHosts"`
Expected: Documentation appears

---

## Acceptance Criteria

- [ ] `parsedURL.Hostname()` used instead of `parsedURL.Host`
- [ ] `AllowPrivateIPsForAllowedHosts` field added to `EsiParserConfig`
- [ ] Hosts in `AllowedHosts` bypass private-IP check when flag is `true`
- [ ] Default behavior unchanged (private IPs blocked regardless of `AllowedHosts`)
- [ ] Tests cover port handling and opt-in flag behavior
- [ ] Godoc updated for `AllowedHosts` and new field
- [ ] README updated with new config option
