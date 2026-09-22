package config

import (
	"errors"
	"testing"
	"time"
)

func TestParseConfigFromJSONMalformed(t *testing.T) {
	cases := []struct {
		name string
		blob string
	}{
		{name: "empty string", blob: ""},
		{name: "truncated object", blob: `{"maxDepth":`},
		{name: "timeoutSeconds as string", blob: `{"timeoutSeconds":"10"}`},
		{name: "fractional maxDepth", blob: `{"maxDepth":1.5}`},
		{name: "blockPrivateIPs as string", blob: `{"blockPrivateIPs":"yes"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseConfigFromJSON([]byte(tc.blob)); err == nil {
				t.Fatalf("ParseConfigFromJSON(%q) = nil error, want error (fail loud, never coerce)", tc.blob)
			}
		})
	}
}

func TestParseConfigDefaults(t *testing.T) {
	c, err := ParseConfigFromJSON([]byte(`{}`))
	if err != nil {
		t.Fatalf("ParseConfigFromJSON({}) = %v", err)
	}

	depth, err := c.ResolvedMaxDepth()
	if err != nil || depth != DefaultMaxDepth {
		t.Errorf("ResolvedMaxDepth() = (%d, %v), want (%d, nil)", depth, err, DefaultMaxDepth)
	}

	timeout, err := c.ResolvedTimeout()
	if err != nil || timeout != 30*time.Second {
		t.Errorf("ResolvedTimeout() = (%v, %v), want (30s, nil)", timeout, err)
	}

	// SSRF blocking defaults ON (secure default).
	if !c.ResolvedBlockPrivateIPs() {
		t.Error("ResolvedBlockPrivateIPs() = false, want true (secure default)")
	}

	if c.AllowPrivateIPsForAllowedHosts {
		t.Error("AllowPrivateIPsForAllowedHosts = true, want false (no bypass by default)")
	}
	if c.DefaultUrl != "" || c.AllowedHosts != "" || c.CacheKeyTemplate != "" {
		t.Errorf("string fields = (%q, %q, %q), want all empty", c.DefaultUrl, c.AllowedHosts, c.CacheKeyTemplate)
	}
	if len(c.RequestCtx) != 0 {
		t.Errorf("RequestCtx = %s, want absent", c.RequestCtx)
	}
}

func TestParseConfigExplicitValues(t *testing.T) {
	c, err := ParseConfigFromJSON([]byte(`{
		"maxDepth": 3,
		"defaultUrl": "http://example.test/",
		"allowedHosts": "backend other.example",
		"blockPrivateIPs": false,
		"allowPrivateIPsForAllowedHosts": true,
		"cacheKeyTemplate": "mesi:${url}",
		"requestCtx": {"headers":{"X-A":"1"},"cookies":[{"name":"a","value":"b"}]},
		"timeoutSeconds": 7
	}`))
	if err != nil {
		t.Fatalf("ParseConfigFromJSON = %v", err)
	}

	depth, err := c.ResolvedMaxDepth()
	if err != nil || depth != 3 {
		t.Errorf("ResolvedMaxDepth() = (%d, %v), want (3, nil)", depth, err)
	}

	timeout, err := c.ResolvedTimeout()
	if err != nil || timeout != 7*time.Second {
		t.Errorf("ResolvedTimeout() = (%v, %v), want (7s, nil)", timeout, err)
	}

	if c.ResolvedBlockPrivateIPs() {
		t.Error("explicit blockPrivateIPs=false not honoured")
	}
	if !c.AllowPrivateIPsForAllowedHosts {
		t.Error("explicit allowPrivateIPsForAllowedHosts=true not honoured")
	}
	if c.DefaultUrl != "http://example.test/" || c.AllowedHosts != "backend other.example" || c.CacheKeyTemplate != "mesi:${url}" {
		t.Errorf("string fields not honoured: %+v", c)
	}
	if len(c.RequestCtx) == 0 {
		t.Error("requestCtx not preserved")
	}
}

func TestParseConfigZeroTimeoutExplicitRejected(t *testing.T) {
	// Explicit 0 must be distinguishable from absent (absent → 30s)
	// and rejected: the core fails every include when Timeout <= 0.
	c, err := ParseConfigFromJSON([]byte(`{"timeoutSeconds":0}`))
	if err != nil {
		t.Fatalf("ParseConfigFromJSON = %v", err)
	}
	if _, err := c.ResolvedTimeout(); err == nil {
		t.Fatal("ResolvedTimeout() with explicit 0 = nil error, want *InvalidTimeoutError")
	}
	var ierr *InvalidTimeoutError
	if err := func() error { _, e := c.ResolvedTimeout(); return e }(); !errors.As(err, &ierr) {
		t.Fatalf("error type = %T, want *InvalidTimeoutError", err)
	}
}

func TestParseConfigZeroMaxDepthExplicitAccepted(t *testing.T) {
	// Explicit 0 is the established passthrough contract (no fetch).
	c, err := ParseConfigFromJSON([]byte(`{"maxDepth":0}`))
	if err != nil {
		t.Fatalf("ParseConfigFromJSON = %v", err)
	}
	depth, err := c.ResolvedMaxDepth()
	if err != nil || depth != 0 {
		t.Errorf("ResolvedMaxDepth() = (%d, %v), want (0, nil)", depth, err)
	}
}

func TestParseConfigRangeBoundaries(t *testing.T) {
	t.Run("timeoutSeconds max accepted", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"timeoutSeconds":86400}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		d, err := c.ResolvedTimeout()
		if err != nil || d != 86400*time.Second {
			t.Errorf("ResolvedTimeout() = (%v, %v), want (86400s, nil)", d, err)
		}
	})

	t.Run("timeoutSeconds max+1 rejected", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"timeoutSeconds":86401}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if _, err := c.ResolvedTimeout(); err == nil {
			t.Fatal("ResolvedTimeout(86401) = nil error, want *InvalidTimeoutError")
		}
	})

	t.Run("negative timeoutSeconds rejected", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"timeoutSeconds":-1}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if _, err := c.ResolvedTimeout(); err == nil {
			t.Fatal("ResolvedTimeout(-1) = nil error, want *InvalidTimeoutError")
		}
	})

	t.Run("maxDepth above MaxMaxDepth rejected", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"maxDepth":10001}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if _, err := c.ResolvedMaxDepth(); err == nil {
			t.Fatal("ResolvedMaxDepth(10001) = nil error, want error")
		}
	})

	t.Run("negative maxDepth rejected", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"maxDepth":-1}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if _, err := c.ResolvedMaxDepth(); err == nil {
			t.Fatal("ResolvedMaxDepth(-1) = nil error, want error")
		}
	})
}

func TestParseConfigUnknownKeysIgnored(t *testing.T) {
	// Forward compatibility: a newer caller may pass keys this build
	// does not know. Unknown keys must not fail the parse; the
	// documented defaults still apply for the known fields.
	c, err := ParseConfigFromJSON([]byte(`{"timeoutsSeconds":10,"somethingNew":[1,2]}`))
	if err != nil {
		t.Fatalf("ParseConfigFromJSON = %v", err)
	}
	d, err := c.ResolvedTimeout()
	if err != nil || d != 30*time.Second {
		t.Errorf("ResolvedTimeout() = (%v, %v), want default (30s, nil)", d, err)
	}
}

func TestParseConfigNullTimeoutTreatedAsAbsent(t *testing.T) {
	// JSON null for a pointer field is the encoding/json "not set"
	// signal — same as an absent key (documented: null = unset → the
	// 30s default, NOT an explicit 0 that would be rejected).
	c, err := ParseConfigFromJSON([]byte(`{"timeoutSeconds":null}`))
	if err != nil {
		t.Fatalf("ParseConfigFromJSON = %v", err)
	}
	d, err := c.ResolvedTimeout()
	if err != nil || d != 30*time.Second {
		t.Errorf("ResolvedTimeout() = (%v, %v), want default (30s, nil)", d, err)
	}
}
