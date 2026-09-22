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
		{name: "maxResponseSize as string", blob: `{"maxResponseSize":"1048576"}`},
		{name: "fractional maxResponseSize", blob: `{"maxResponseSize":1.5}`},
		{name: "maxResponseSize above int64 range", blob: `{"maxResponseSize":9223372036854775808}`},
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

	// Absent maxResponseSize → 0 (unlimited) — byte-identical to the
	// positional Parse* paths, NOT the 10 MB of CreateDefaultConfig.
	maxResp, err := c.ResolvedMaxResponseSize()
	if err != nil || maxResp != 0 {
		t.Errorf("ResolvedMaxResponseSize() = (%d, %v), want (0, nil)", maxResp, err)
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
		"timeoutSeconds": 7,
		"maxResponseSize": 2048
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

	maxResp, err := c.ResolvedMaxResponseSize()
	if err != nil || maxResp != 2048 {
		t.Errorf("ResolvedMaxResponseSize() = (%d, %v), want (2048, nil)", maxResp, err)
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

	t.Run("maxResponseSize accepted max", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"maxResponseSize":9223372036854775806}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		v, err := c.ResolvedMaxResponseSize()
		if err != nil || v != MaxMaxResponseSize {
			t.Errorf("ResolvedMaxResponseSize() = (%d, %v), want (%d, nil)", v, err, MaxMaxResponseSize)
		}
	})

	t.Run("maxResponseSize max+1 rejected", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"maxResponseSize":9223372036854775807}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if _, err := c.ResolvedMaxResponseSize(); err == nil {
			t.Fatal("ResolvedMaxResponseSize(math.MaxInt64) = nil error, want *InvalidMaxResponseSizeError")
		}
		var ierr *InvalidMaxResponseSizeError
		if err := func() error { _, e := c.ResolvedMaxResponseSize(); return e }(); !errors.As(err, &ierr) {
			t.Fatalf("error type = %T, want *InvalidMaxResponseSizeError", err)
		}
	})

	t.Run("negative maxResponseSize rejected", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"maxResponseSize":-1}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if _, err := c.ResolvedMaxResponseSize(); err == nil {
			t.Fatal("ResolvedMaxResponseSize(-1) = nil error, want *InvalidMaxResponseSizeError")
		}
	})

	t.Run("explicit maxResponseSize 0 accepted as unlimited", func(t *testing.T) {
		// Unlike timeoutSeconds, 0 is a legitimate documented value
		// (unlimited) — it must survive the round-trip, not be
		// rejected and not be confused with an absent key.
		c, err := ParseConfigFromJSON([]byte(`{"maxResponseSize":0}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if c.MaxResponseSize == nil {
			t.Fatal("MaxResponseSize pointer is nil for explicit 0 — key must be distinguishable from absent")
		}
		v, err := c.ResolvedMaxResponseSize()
		if err != nil || v != 0 {
			t.Errorf("ResolvedMaxResponseSize() = (%d, %v), want (0, nil)", v, err)
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

func TestParseConfigNullMaxResponseSizeTreatedAsAbsent(t *testing.T) {
	// null is the encoding/json "not set" signal — same as an absent
	// key (documented: null = unset → 0 = unlimited), NOT an explicit
	// value that could fail validation.
	c, err := ParseConfigFromJSON([]byte(`{"maxResponseSize":null}`))
	if err != nil {
		t.Fatalf("ParseConfigFromJSON = %v", err)
	}
	v, err := c.ResolvedMaxResponseSize()
	if err != nil || v != 0 {
		t.Errorf("ResolvedMaxResponseSize() = (%d, %v), want default (0, nil)", v, err)
	}
}
