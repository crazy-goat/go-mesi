package config

import (
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestValidateTimeoutAccepted(t *testing.T) {
	cases := []struct {
		name  string
		value int
	}{
		{name: "minimum 1", value: 1},
		{name: "issue example 10", value: 10},
		{name: "default 30", value: DefaultTimeoutSeconds},
		{name: "accepted max", value: MaxTimeoutSeconds},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateTimeout(tc.value); err != nil {
				t.Fatalf("ValidateTimeout(%d) = %v, want nil", tc.value, err)
			}
		})
	}
}

func TestValidateTimeoutRejected(t *testing.T) {
	cases := []struct {
		name  string
		value int
		why   string
	}{
		{name: "zero", value: 0, why: "value 0 is below minimum 1"},
		{name: "negative", value: -1, why: "value -1 is below minimum 1"},
		{name: "rejected at max+1", value: MaxTimeoutSeconds + 1, why: "value 86401 exceeds maximum 86400"},
		{name: "huge value", value: 999999999, why: "value 999999999 exceeds maximum 86400"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTimeout(tc.value)
			if err == nil {
				t.Fatalf("ValidateTimeout(%d) = nil, want *InvalidTimeoutError", tc.value)
			}
			var ierr *InvalidTimeoutError
			if !errors.As(err, &ierr) {
				t.Fatalf("error type = %T, want *InvalidTimeoutError", err)
			}
			if want := strconv.Itoa(tc.value); ierr.Input != want {
				t.Errorf("InvalidTimeoutError.Input = %q, want %q", ierr.Input, want)
			}
			if ierr.Why != tc.why {
				t.Errorf("InvalidTimeoutError.Why = %q, want %q", ierr.Why, tc.why)
			}
		})
	}
}

func TestResolveTimeout(t *testing.T) {
	t.Run("absent falls back to the documented 30s default", func(t *testing.T) {
		d, err := ResolveTimeout(nil)
		if err != nil {
			t.Fatalf("ResolveTimeout(nil) = %v, want nil", err)
		}
		if d != 30*time.Second {
			t.Errorf("ResolveTimeout(nil) = %v, want 30s", d)
		}
	})

	t.Run("explicit value wins", func(t *testing.T) {
		v := 10
		d, err := ResolveTimeout(&v)
		if err != nil {
			t.Fatalf("ResolveTimeout(10) = %v, want nil", err)
		}
		if d != 10*time.Second {
			t.Errorf("ResolveTimeout(10) = %v, want 10s", d)
		}
	})

	t.Run("explicit 0 is an error, not the default", func(t *testing.T) {
		v := 0
		if _, err := ResolveTimeout(&v); err == nil {
			t.Fatal("ResolveTimeout(0) = nil error, want *InvalidTimeoutError")
		}
	})

	t.Run("explicit out-of-range is an error, not the default", func(t *testing.T) {
		v := MaxTimeoutSeconds + 1
		if _, err := ResolveTimeout(&v); err == nil {
			t.Fatalf("ResolveTimeout(%d) = nil error, want *InvalidTimeoutError", v)
		}
	})

	t.Run("explicit minimum 1 accepted", func(t *testing.T) {
		v := 1
		d, err := ResolveTimeout(&v)
		if err != nil || d != time.Second {
			t.Errorf("ResolveTimeout(1) = (%v, %v), want (1s, nil)", d, err)
		}
	})

	t.Run("explicit maximum accepted", func(t *testing.T) {
		v := MaxTimeoutSeconds
		d, err := ResolveTimeout(&v)
		if err != nil || d != MaxTimeoutSeconds*time.Second {
			t.Errorf("ResolveTimeout(%d) = (%v, %v), want (%ds, nil)", v, d, err, MaxTimeoutSeconds)
		}
	})
}

func TestTimeoutDocumentedConstants(t *testing.T) {
	// Pins the values documented in the Apache README, CHANGELOG and
	// examples/apache-timeout.conf — and mirrored by
	// MESI_DEFAULT_TIMEOUT_SECONDS / MESI_MAX_TIMEOUT_SECONDS in
	// servers/apache/mod_mesi.c (#167). Changing either constant here
	// without updating those docs must fail.
	if DefaultTimeoutSeconds != 30 {
		t.Errorf("DefaultTimeoutSeconds = %d, want 30 (libgomesi's historical hardcoded value)", DefaultTimeoutSeconds)
	}
	if MaxTimeoutSeconds != 86400 {
		t.Errorf("MaxTimeoutSeconds = %d, want 86400 (24h, mirrors MesiCacheTTL ceiling)", MaxTimeoutSeconds)
	}
}
