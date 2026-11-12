package traefik

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestREADMEConfigurationOptions(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(data)

	t.Run("general options section", func(t *testing.T) {
		_, section, ok := strings.Cut(readme, "\n## Configuration Options\n")
		if !ok {
			t.Fatal("general options table must have a top-level Configuration Options heading, not be nested under a cache backend")
		}
		section, _, _ = strings.Cut(section, "\n## ")
		if !strings.Contains(section, "| `maxDepth` |") {
			t.Error("Configuration Options section must contain the general options table")
		}
	})

	t.Run("blockPrivateIPs row", func(t *testing.T) {
		row := regexp.MustCompile("(?m)^\\| *`?blockPrivateIPs`? *\\| *`?bool`? *\\| *`?true`? *\\|([^\\n]+)\\| *$").FindStringSubmatch(readme)
		if row == nil {
			t.Fatal("configuration table must document blockPrivateIPs with type bool and default true")
		}
		for _, caveat := range []string{"Yaegi", "dial-time", "not enforced", "allowedHosts"} {
			if !strings.Contains(row[1], caveat) {
				t.Errorf("blockPrivateIPs description must include %q", caveat)
			}
		}
	})
}
