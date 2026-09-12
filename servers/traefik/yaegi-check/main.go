package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("PASS: Yaegi compatibility check passed")
}

func run() error {
	root, err := findRepoRoot()
	if err != nil {
		return fmt.Errorf("cannot find repo root: %w", err)
	}

	gopath, err := os.MkdirTemp("", "yaegi-check-*")
	if err != nil {
		return fmt.Errorf("cannot create temp gopath: %w", err)
	}
	defer os.RemoveAll(gopath)

	if err := copyPluginSources(root, gopath); err != nil {
		return fmt.Errorf("cannot copy plugin sources: %w", err)
	}

	i := interp.New(interp.Options{GoPath: gopath})
	if err := i.Use(stdlib.Symbols); err != nil {
		return fmt.Errorf("cannot load stdlib: %w", err)
	}

	_, err = i.Eval(`import "github.com/crazy-goat/go-mesi/mesi"`)
	if err != nil {
		return fmt.Errorf("Yaegi cannot import mesi package:\n  %v", err)
	}

	return nil
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("not in a git repository")
		}
		dir = parent
	}
}

func copyPluginSources(root, gopath string) error {
	srcDir := filepath.Join(gopath, "src", "github.com", "crazy-goat", "go-mesi")

	entries := []struct {
		src string
		dst string
		skipPrefixes []string
	}{
		{
			src: filepath.Join(root, "mesi"),
			dst: filepath.Join(srcDir, "mesi"),
			skipPrefixes: []string{
				"ssrf_dialer.go",
				"cache_redis",
				"cache_memcached",
				"_test.go",
			},
		},
		{
			src: filepath.Join(root, "middleware"),
			dst: filepath.Join(srcDir, "middleware"),
			skipPrefixes: nil,
		},
		{
			src: filepath.Join(root, "servers", "traefik"),
			dst: filepath.Join(srcDir, "servers", "traefik"),
			skipPrefixes: []string{
				"_test.go",
				"cache_redis.go",
				"cache_memcached.go",
				"mesi_memcached",
			},
		},
	}

	for _, e := range entries {
		if err := copyDir(e.src, e.dst, e.skipPrefixes); err != nil {
			return fmt.Errorf("copying %s: %w", e.src, err)
		}
	}

	if err := copyFile(
		filepath.Join(root, "servers", "traefik", "go.mod"),
		filepath.Join(srcDir, "servers", "traefik", "go.mod"),
	); err != nil {
		return err
	}

	// Provide a stub NewSSRFSafeTransport for Yaegi (no dial-time IP blocking).
	// The real implementation in ssrf_dialer.go uses syscall.RawConn which Yaegi
	// cannot interpret. This matches the Dockerfile approach.
	stub := `package mesi

import "net/http"

func NewSSRFSafeTransport(config EsiParserConfig) *http.Transport {
	return &http.Transport{}
}
`
	if err := os.WriteFile(
		filepath.Join(srcDir, "mesi", "ssrf_yaegi.go"),
		[]byte(stub),
		0644,
	); err != nil {
		return fmt.Errorf("cannot write ssrf stub: %w", err)
	}

	return nil
}

func copyDir(src, dst string, skipPrefixes []string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		if shouldSkip(name, skipPrefixes) {
			continue
		}

		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath, skipPrefixes); err != nil {
				return err
			}
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func shouldSkip(name string, skipPrefixes []string) bool {
	for _, p := range skipPrefixes {
		if strings.HasPrefix(name, p) || strings.HasSuffix(name, p) {
			return true
		}
	}
	return false
}
