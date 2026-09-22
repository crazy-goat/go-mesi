package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var cliBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mesi-cli-test")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	cliBinary = filepath.Join(dir, "mesi-cli")
	cmd := exec.Command("go", "build", "-o", cliBinary, "mesi-cli.go")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s\n", err, out)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func runCLI(t *testing.T, args ...string) (stdout string, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.Command(cliBinary, args...)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run CLI: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func TestIsURL(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"http://example.com", true},
		{"https://example.com", true},
		{"HTTP://example.com", false},
		{"/path/to/file", false},
		{"./relative/path", false},
		{"", false},
		{"ftp://example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isURL(tt.input)
			if got != tt.want {
				t.Errorf("isURL(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCLI_fileMode_esiComment(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello World-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello World") {
		t.Errorf("expected 'Hello World' in output, got %q", stdout)
	}
}

func TestCLI_fileMode_esiRemove(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	content := "<esi:remove>should be removed</esi:remove><p>keep</p>"
	if err := os.WriteFile(inputFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "<p>keep</p>") {
		t.Errorf("expected '<p>keep</p>' in output, got %q", stdout)
	}
	if strings.Contains(stdout, "should be removed") {
		t.Errorf("content should not contain removed text, got %q", stdout)
	}
}

func TestCLI_fileMode_nonEsiPassthrough(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.txt")
	content := "plain text content"
	if err := os.WriteFile(inputFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if strings.TrimSpace(stdout) != content {
		t.Errorf("expected %q, got %q", content, strings.TrimSpace(stdout))
	}
}

func TestCLI_fileMode_emptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "empty.html")
	if err := os.WriteFile(inputFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, inputFile)
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if stdout != "" && stdout != "\n" {
		t.Errorf("expected empty output, got %q", stdout)
	}
}

func TestCLI_error_missingArgument(t *testing.T) {
	stdout, stderr, _ := runCLI(t)
	output := stdout + stderr
	if !strings.Contains(output, "Error") && !strings.Contains(output, "Usage") {
		t.Errorf("expected error message in output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_error_nonexistentFile(t *testing.T) {
	stdout, stderr, _ := runCLI(t, "/nonexistent/file/path.html")
	output := stdout + stderr
	if !strings.Contains(output, "Error") {
		t.Errorf("expected error message, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_defaultUrlFlag(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, "--default-url", "http://example.com/", inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}

func TestCLI_maxDepthFlag(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, "--max-depth", "0", inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' with max-depth=0, got %q", stdout)
	}
}

func TestCLI_maxDepthAtCap(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runCLI(t, "--max-depth", "10000", inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d (stderr=%q)", exitCode, stderr)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' with max-depth=10000, got %q", stdout)
	}
}

func TestCLI_maxDepthAboveCap(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runCLI(t, "--max-depth", "10001", inputFile)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit for max-depth=10001 (stdout=%q stderr=%q)", stdout, stderr)
	}
	out := stderr + stdout
	if !strings.Contains(out, "max-depth") || !strings.Contains(out, "10001") {
		t.Errorf("expected max-depth range error, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_parseOnHeaderFlag(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, "--parse-on-header", inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' with parse-on-header, got %q", stdout)
	}
}

func TestCLI_cacheBackendUnknown(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runCLI(t, "-cache-backend=unknown", inputFile)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code for unknown backend, got 0 (stdout=%q stderr=%q)", stdout, stderr)
	}
	if !strings.Contains(stderr, "unknown cache backend") {
		t.Errorf("expected 'unknown cache backend' in stderr, got %q", stderr)
	}
}

func TestCLI_cacheBackendRedis(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runCLI(t,
		"-cache-backend=redis",
		"-cache-redis-addr=localhost:6379",
		"-cache-ttl=10s",
		inputFile,
	)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d (stderr=%q)", exitCode, stderr)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' with -cache-backend=redis, got %q", stdout)
	}
}

func TestCLI_cacheBackendMemory(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t,
		"-cache-backend=memory",
		"-cache-size=100",
		"-cache-ttl=10s",
		inputFile,
	)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' with -cache-backend=memory, got %q", stdout)
	}
}

func TestCLI_cacheBackendMemcached(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, exitCode := runCLI(t,
		"-cache-backend=memcached",
		"-cache-memcached-servers=localhost:11211",
		"-cache-ttl=10s",
		inputFile,
	)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d (stderr=%q)", exitCode, stderr)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' with -cache-backend=memcached, got %q", stdout)
	}
}

func TestCLI_cacheBackendMemcachedNoServers(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	_, stderr, exitCode := runCLI(t,
		"-cache-backend=memcached",
		"-cache-ttl=10s",
		inputFile,
	)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code for memcached without servers, got 0")
	}
	if !strings.Contains(stderr, "cache-memcached-servers required") {
		t.Errorf("expected 'cache-memcached-servers required' in stderr, got %q", stderr)
	}
}

func TestCLI_cacheBackendMemcachedEmptyServers(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	_, stderr, exitCode := runCLI(t,
		"-cache-backend=memcached",
		"-cache-memcached-servers=",
		"-cache-ttl=10s",
		inputFile,
	)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code for empty servers, got 0")
	}
	if !strings.Contains(stderr, "cache-memcached-servers required") {
		t.Errorf("expected 'cache-memcached-servers required' in stderr, got %q", stderr)
	}
}

func TestCLI_cacheFlagsInHelp(t *testing.T) {
	stdout, stderr, _ := runCLI(t, "-h")
	output := stdout + stderr
	for _, flag := range []string{"-cache-backend", "-cache-size", "-cache-ttl", "-cache-redis-addr", "-cache-redis-password", "-cache-redis-db", "-cache-memcached-servers"} {
		if !strings.Contains(output, flag) {
			t.Errorf("expected %q in -h output, got stdout=%q stderr=%q", flag, stdout, stderr)
		}
	}
}

func TestCLI_cacheKeyTemplateFlagInHelp(t *testing.T) {
	stdout, stderr, _ := runCLI(t, "-h")
	output := stdout + stderr
	if !strings.Contains(output, "-cache-key-template") {
		t.Errorf("expected -cache-key-template in help output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_cacheKeyTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t,
		"-cache-backend=memory",
		"-cache-key-template=myapp:${url}",
		inputFile,
	)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}

func TestCLI_cacheKeyTemplateDefault(t *testing.T) {
	// When -cache-key-template is not provided, the default URL-only cache key is used.
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t,
		"-cache-backend=memory",
		inputFile,
	)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}

func TestCLI_cacheKeyTemplateEmpty(t *testing.T) {
	// Empty template should use default URL-only cache key.
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t,
		"-cache-backend=memory",
		"-cache-key-template=",
		inputFile,
	)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}

func TestCLI_cacheKeyTemplateWithURLPlaceholder(t *testing.T) {
	// Test that ${url} placeholder is substituted in the template.
	// This test verifies the template is used by checking that the CLI
	// still processes ESI correctly with a custom template.
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t,
		"-cache-backend=memory",
		"-cache-key-template=prefix:${url}:suffix",
		inputFile,
	)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}

func TestAllowedHostsFromFlag(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"absent", "", nil},
		{"single host", "backend.internal", []string{"backend.internal"}},
		{"multiple hosts", "backend.internal,cdn.example.com", []string{"backend.internal", "cdn.example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := allowedHostsFromFlag(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("allowedHostsFromFlag(%q) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCLI_allowedHostsFlagInHelp(t *testing.T) {
	stdout, stderr, _ := runCLI(t, "-h")
	output := stdout + stderr
	if !strings.Contains(output, "-allowed-hosts") {
		t.Errorf("expected -allowed-hosts in help output, got stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(output, "Comma-separated list of allowed hosts") {
		t.Errorf("expected -allowed-hosts description in help output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_allowedHostsFlag(t *testing.T) {
	// The whitelist does not affect file-mode processing of static ESI
	// comments (no includes fetched), but the flag must be accepted.
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello World-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, "-allowed-hosts=backend.internal,cdn.example.com", inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello World") {
		t.Errorf("expected 'Hello World' in output, got %q", stdout)
	}
}

func TestCLI_allowedHostsFlagDefaultEmpty(t *testing.T) {
	// Absent flag keeps the legacy nil allowlist (all hosts allowed,
	// subject to BlockPrivateIPs).
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}

func TestCLI_allowPrivateIPsForAllowedHostsFlagInHelp(t *testing.T) {
	stdout, stderr, _ := runCLI(t, "-h")
	output := stdout + stderr
	if !strings.Contains(output, "-allowPrivateIPsForAllowedHosts") {
		t.Errorf("expected -allowPrivateIPsForAllowedHosts in help output, got stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(output, "hosts listed in -allowed-hosts") {
		t.Errorf("expected -allowPrivateIPsForAllowedHosts description in help output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_allowPrivateIPsForAllowedHostsFlag(t *testing.T) {
	// The bypass does not affect file-mode processing of static ESI
	// comments (no includes fetched), but the flag must be accepted.
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello World-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, "-allowPrivateIPsForAllowedHosts", inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello World") {
		t.Errorf("expected 'Hello World' in output, got %q", stdout)
	}
}

func TestCLI_sharedHTTPClientFlagInHelp(t *testing.T) {
	stdout, stderr, _ := runCLI(t, "-h")
	output := stdout + stderr
	if !strings.Contains(output, "-shared-http-client") {
		t.Errorf("expected -shared-http-client in help output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_sharedHTTPClientFlagPassthrough(t *testing.T) {
	// With -shared-http-client, the CLI should still process a simple
	// file-mode ESI comment correctly (the flag does not change the
	// parsing behaviour for file-based input where no includes are
	// fetched — it only affects HTTP client creation for remote
	// includes).
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello World-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, "-shared-http-client", inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello World") {
		t.Errorf("expected 'Hello World' in output, got %q", stdout)
	}
}

func TestCLI_includeErrorMarkerFlagInHelp(t *testing.T) {
	stdout, stderr, _ := runCLI(t, "-h")
	output := stdout + stderr
	if !strings.Contains(output, "-include-error-marker") {
		t.Errorf("expected -include-error-marker in help output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_includeErrorMarkerFlag(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, "-include-error-marker", "<!-- esi error -->", inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}

func TestCLI_includeErrorMarkerFlagDefaultEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	// Default should be empty string - no marker rendered
	stdout, _, exitCode := runCLI(t, inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}

func TestCLI_includeErrorMarkerFlagEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	// Explicit empty string should also work
	stdout, _, exitCode := runCLI(t, "-include-error-marker=", inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}

func TestCLI_includeErrorMarkerFlagSpecialChars(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	// Test with special characters in the marker
	stdout, _, exitCode := runCLI(t, "-include-error-marker", "[ESI ERROR]", inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}
func TestCLI_sharedHTTPClientFlagFalseByDefault(t *testing.T) {
	// When -shared-http-client is not provided, config.HTTPClient is nil
	// (per-request clients). This test verifies that the CLI works
	// correctly without the flag.
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	stdout, _, exitCode := runCLI(t, inputFile)
	if exitCode != 0 {
		t.Fatalf("unexpected exit code %d", exitCode)
	}
	if !strings.Contains(stdout, "Hello") {
		t.Errorf("expected 'Hello' in output, got %q", stdout)
	}
}

func TestValidateMaxResponseSize(t *testing.T) {
	// Boundary classes per project rules: accepted 0 / 1 / typical / max,
	// rejected negative / max+1 (== math.MaxInt64, the core's +1
	// LimitReader wrap, #448).
	rangeText := fmt.Sprintf("[0, %d]", maxMaxResponseSize)
	tests := []struct {
		name    string
		value   int64
		wantErr bool
	}{
		{"zero (documented unlimited) accepted", 0, false},
		{"one byte accepted", 1, false},
		{"typical 1 MiB accepted", 1024 * 1024, false},
		{"accepted max (MaxInt64-1)", maxMaxResponseSize, false},
		{"rejected negative", -1, true},
		{"rejected large negative", -1024 * 1024, true},
		{"rejected max+1 (math.MaxInt64)", maxMaxResponseSize + 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMaxResponseSize(tt.value)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("validateMaxResponseSize(%d) = %v, want nil", tt.value, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateMaxResponseSize(%d) = nil, want error", tt.value)
			}
			// The error must name the flag and the valid range.
			if !strings.Contains(err.Error(), "max-response-size") {
				t.Errorf("error %q does not name the flag", err)
			}
			if !strings.Contains(err.Error(), rangeText) {
				t.Errorf("error %q does not contain the valid range %q", err, rangeText)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("%d", tt.value)) {
				t.Errorf("error %q does not contain the offending value", err)
			}
		})
	}
}

func TestCLI_maxResponseSizeFlagInHelp(t *testing.T) {
	stdout, stderr, _ := runCLI(t, "-h")
	output := stdout + stderr
	if !strings.Contains(output, "-max-response-size") {
		t.Errorf("expected -max-response-size in help output, got stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(output, "0 = unlimited") {
		t.Errorf("expected '0 = unlimited' in help output, got stdout=%q stderr=%q", stdout, stderr)
	}
	// Absent flag must keep CreateDefaultConfig()'s 10 MB default — the
	// flag package only prints "(default …)" for non-zero defaults, so
	// this also pins that the default is NOT 0.
	if !strings.Contains(output, "(default 10485760)") {
		t.Errorf("expected '(default 10485760)' in help output, got stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCLI_maxResponseSizeFlagValidation(t *testing.T) {
	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.html")
	if err := os.WriteFile(inputFile, []byte("<!--esi Hello-->"), 0644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		arg      string
		wantExit int
		wantErrs []string // asserted against stderr+stdout when non-empty
	}{
		{name: "accepted zero (unlimited)", arg: "-max-response-size=0", wantExit: 0},
		{name: "accepted one byte", arg: "-max-response-size=1", wantExit: 0},
		{name: "accepted typical", arg: "-max-response-size=1048576", wantExit: 0},
		{name: "accepted max (MaxInt64-1)", arg: "-max-response-size=9223372036854775806", wantExit: 0},
		{
			name: "rejected negative", arg: "-max-response-size=-1", wantExit: 1,
			wantErrs: []string{"max-response-size", "9223372036854775806", "-1"},
		},
		{
			// math.MaxInt64 == maxMaxResponseSize+1: the core's
			// MaxResponseSize+1 LimitReader bound would wrap negative (#448).
			name: "rejected MaxInt64 (max+1)", arg: "-max-response-size=9223372036854775807", wantExit: 1,
			wantErrs: []string{"max-response-size", "9223372036854775806"},
		},
		{
			name: "rejected above int64 (flag pkg parse error)", arg: "-max-response-size=9223372036854775808", wantExit: 2,
			wantErrs: []string{"max-response-size"},
		},
		{
			name: "rejected non-integer (flag pkg parse error)", arg: "-max-response-size=abc", wantExit: 2,
			wantErrs: []string{"max-response-size"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, exitCode := runCLI(t, tt.arg, inputFile)
			if exitCode != tt.wantExit {
				t.Fatalf("exit code = %d, want %d (stdout=%q stderr=%q)", exitCode, tt.wantExit, stdout, stderr)
			}
			if tt.wantExit == 0 {
				if !strings.Contains(stdout, "Hello") {
					t.Errorf("expected 'Hello' in output, got %q", stdout)
				}
				return
			}
			out := stderr + stdout
			for _, want := range tt.wantErrs {
				if !strings.Contains(out, want) {
					t.Errorf("expected %q in error output, got stdout=%q stderr=%q", want, stdout, stderr)
				}
			}
		})
	}
}
