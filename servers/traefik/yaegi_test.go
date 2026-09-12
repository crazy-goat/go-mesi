package traefik

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func findRepoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to find repo root: %v", err)
	}
	return strings.TrimSpace(out.String())
}

func TestYaegiCompatibility(t *testing.T) {
	root := findRepoRoot(t)

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = filepath.Join(root, "servers", "traefik", "yaegi-check")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Yaegi compatibility check failed:\n%s\nError: %v", out, err)
	}
	t.Logf("Yaegi check output:\n%s", out)
}
