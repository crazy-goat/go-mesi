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
		{name: "maxConcurrentRequests as string", blob: `{"maxConcurrentRequests":"5"}`},
		{name: "fractional maxConcurrentRequests", blob: `{"maxConcurrentRequests":1.5}`},
		{name: "maxConcurrentRequests above int64 range", blob: `{"maxConcurrentRequests":9223372036854775808}`},
		{name: "maxWorkers as string", blob: `{"maxWorkers":"4"}`},
		{name: "fractional maxWorkers", blob: `{"maxWorkers":1.5}`},
		{name: "maxWorkers above int64 range", blob: `{"maxWorkers":9223372036854775808}`},
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

	// Absent maxConcurrentRequests → 0 (unlimited) — byte-identical to
	// the positional Parse* paths, which leave
	// EsiParserConfig.MaxConcurrentRequests at its zero value.
	maxConc, err := c.ResolvedMaxConcurrentRequests()
	if err != nil || maxConc != 0 {
		t.Errorf("ResolvedMaxConcurrentRequests() = (%d, %v), want (0, nil)", maxConc, err)
	}

	// Absent maxWorkers → 0 (library default NumCPU*4) — byte-identical
	// to the positional Parse* paths, which leave
	// EsiParserConfig.MaxWorkers at its zero value.
	maxWorkers, err := c.ResolvedMaxWorkers()
	if err != nil || maxWorkers != 0 {
		t.Errorf("ResolvedMaxWorkers() = (%d, %v), want (0, nil)", maxWorkers, err)
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
		"maxResponseSize": 2048,
		"maxConcurrentRequests": 5,
		"maxWorkers": 4
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

	maxConc, err := c.ResolvedMaxConcurrentRequests()
	if err != nil || maxConc != 5 {
		t.Errorf("ResolvedMaxConcurrentRequests() = (%d, %v), want (5, nil)", maxConc, err)
	}

	maxWorkers, err := c.ResolvedMaxWorkers()
	if err != nil || maxWorkers != 4 {
		t.Errorf("ResolvedMaxWorkers() = (%d, %v), want (4, nil)", maxWorkers, err)
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

	t.Run("maxConcurrentRequests accepted max", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"maxConcurrentRequests":999999999}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		v, err := c.ResolvedMaxConcurrentRequests()
		if err != nil || v != MaxMaxConcurrentRequests {
			t.Errorf("ResolvedMaxConcurrentRequests() = (%d, %v), want (%d, nil)", v, err, MaxMaxConcurrentRequests)
		}
	})

	t.Run("maxConcurrentRequests max+1 rejected", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"maxConcurrentRequests":1000000000}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if _, err := c.ResolvedMaxConcurrentRequests(); err == nil {
			t.Fatal("ResolvedMaxConcurrentRequests(1000000000) = nil error, want *InvalidMaxConcurrentRequestsError")
		}
		var ierr *InvalidMaxConcurrentRequestsError
		if err := func() error { _, e := c.ResolvedMaxConcurrentRequests(); return e }(); !errors.As(err, &ierr) {
			t.Fatalf("error type = %T, want *InvalidMaxConcurrentRequestsError", err)
		}
	})

	t.Run("negative maxConcurrentRequests rejected", func(t *testing.T) {
		// The core only warns and normalizes a negative to 0 (#329) —
		// libgomesi must fail loud instead: a malformed explicit value
		// must never pass as the documented "unlimited".
		c, err := ParseConfigFromJSON([]byte(`{"maxConcurrentRequests":-1}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if _, err := c.ResolvedMaxConcurrentRequests(); err == nil {
			t.Fatal("ResolvedMaxConcurrentRequests(-1) = nil error, want *InvalidMaxConcurrentRequestsError")
		}
	})

	t.Run("explicit maxConcurrentRequests 0 accepted as unlimited", func(t *testing.T) {
		// 0 is a legitimate documented value (unlimited) — it must
		// survive the round-trip, not be rejected and not be confused
		// with an absent key (Apache's -1-sentinel merge relies on the
		// explicit 0 reaching the core untouched).
		c, err := ParseConfigFromJSON([]byte(`{"maxConcurrentRequests":0}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if c.MaxConcurrentRequests == nil {
			t.Fatal("MaxConcurrentRequests pointer is nil for explicit 0 — key must be distinguishable from absent")
		}
		v, err := c.ResolvedMaxConcurrentRequests()
		if err != nil || v != 0 {
			t.Errorf("ResolvedMaxConcurrentRequests() = (%d, %v), want (0, nil)", v, err)
		}
	})

	t.Run("maxWorkers accepted max", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"maxWorkers":999999999}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		v, err := c.ResolvedMaxWorkers()
		if err != nil || v != MaxMaxWorkers {
			t.Errorf("ResolvedMaxWorkers() = (%d, %v), want (%d, nil)", v, err, MaxMaxWorkers)
		}
	})

	t.Run("maxWorkers max+1 rejected", func(t *testing.T) {
		c, err := ParseConfigFromJSON([]byte(`{"maxWorkers":1000000000}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if _, err := c.ResolvedMaxWorkers(); err == nil {
			t.Fatal("ResolvedMaxWorkers(1000000000) = nil error, want *InvalidMaxWorkersError")
		}
		var ierr *InvalidMaxWorkersError
		if err := func() error { _, e := c.ResolvedMaxWorkers(); return e }(); !errors.As(err, &ierr) {
			t.Fatalf("error type = %T, want *InvalidMaxWorkersError", err)
		}
	})

	t.Run("negative maxWorkers rejected", func(t *testing.T) {
		// The core silently substitutes runtime.NumCPU()*4 for any
		// value <= 0 with NO warning (mesi/parser.go — there is no
		// #329-style warn+normalize here) — libgomesi must fail loud
		// instead: a malformed explicit value must never pass as the
		// documented "library default".
		c, err := ParseConfigFromJSON([]byte(`{"maxWorkers":-1}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if _, err := c.ResolvedMaxWorkers(); err == nil {
			t.Fatal("ResolvedMaxWorkers(-1) = nil error, want *InvalidMaxWorkersError")
		}
	})

	t.Run("explicit maxWorkers 0 accepted as library default", func(t *testing.T) {
		// 0 is a legitimate documented value (library default — the
		// core substitutes NumCPU*4 for <= 0) — it must survive the
		// round-trip, not be rejected and not be confused with an
		// absent key (Apache's -1-sentinel merge renders the key only
		// for an explicitly configured 0, so the distinction has to
		// hold through the schema too).
		c, err := ParseConfigFromJSON([]byte(`{"maxWorkers":0}`))
		if err != nil {
			t.Fatalf("ParseConfigFromJSON = %v", err)
		}
		if c.MaxWorkers == nil {
			t.Fatal("MaxWorkers pointer is nil for explicit 0 — key must be distinguishable from absent")
		}
		v, err := c.ResolvedMaxWorkers()
		if err != nil || v != 0 {
			t.Errorf("ResolvedMaxWorkers() = (%d, %v), want (0, nil)", v, err)
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

func TestParseConfigNullMaxConcurrentRequestsTreatedAsAbsent(t *testing.T) {
	// null is the encoding/json "not set" signal — same as an absent
	// key (documented: null = unset → 0 = unlimited), NOT an explicit
	// value that could fail validation.
	c, err := ParseConfigFromJSON([]byte(`{"maxConcurrentRequests":null}`))
	if err != nil {
		t.Fatalf("ParseConfigFromJSON = %v", err)
	}
	if c.MaxConcurrentRequests != nil {
		t.Fatal("MaxConcurrentRequests pointer non-nil for null — key must be distinguishable from absent")
	}
	v, err := c.ResolvedMaxConcurrentRequests()
	if err != nil || v != 0 {
		t.Errorf("ResolvedMaxConcurrentRequests() = (%d, %v), want default (0, nil)", v, err)
	}
}

func TestParseConfigNullMaxWorkersTreatedAsAbsent(t *testing.T) {
	// null is the encoding/json "not set" signal — same as an absent
	// key (documented: null = unset → 0 = library default), NOT an
	// explicit value that could fail validation.
	c, err := ParseConfigFromJSON([]byte(`{"maxWorkers":null}`))
	if err != nil {
		t.Fatalf("ParseConfigFromJSON = %v", err)
	}
	if c.MaxWorkers != nil {
		t.Fatal("MaxWorkers pointer non-nil for null — key must be distinguishable from absent")
	}
	v, err := c.ResolvedMaxWorkers()
	if err != nil || v != 0 {
		t.Errorf("ResolvedMaxWorkers() = (%d, %v), want default (0, nil)", v, err)
	}
}
