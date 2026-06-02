package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	os.RemoveAll(dir)
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
	stdout, stderr, exitCode := runCLI(t, "-cache-backend=redis", inputFile)
	if exitCode == 0 {
		t.Fatalf("expected non-zero exit code for unknown backend, got 0 (stdout=%q stderr=%q)", stdout, stderr)
	}
	if !strings.Contains(stderr, "unknown cache backend") {
		t.Errorf("expected 'unknown cache backend' in stderr, got %q", stderr)
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

func TestCLI_cacheFlagsInHelp(t *testing.T) {
	stdout, stderr, _ := runCLI(t, "-h")
	output := stdout + stderr
	for _, flag := range []string{"-cache-backend", "-cache-size", "-cache-ttl"} {
		if !strings.Contains(output, flag) {
			t.Errorf("expected %q in -h output, got stdout=%q stderr=%q", flag, stdout, stderr)
		}
	}
}
