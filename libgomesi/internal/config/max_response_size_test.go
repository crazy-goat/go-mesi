package config

import (
	"errors"
	"fmt"
	"math"
	"testing"
)

func TestMaxMaxResponseSizeConstant(t *testing.T) {
	// The cap is derived from the core's io.LimitReader(body,
	// MaxResponseSize+1) bound (mesi/fetch.go) — at math.MaxInt64 the
	// +1 would wrap negative and the include would silently read an
	// empty body instead of failing. Pin the exact value so a future
	// change to the bound is a deliberate one.
	if MaxMaxResponseSize != math.MaxInt64-1 {
		t.Errorf("MaxMaxResponseSize = %d, want %d", MaxMaxResponseSize, int64(math.MaxInt64)-1)
	}
}

func TestValidateMaxResponseSizeAccepted(t *testing.T) {
	cases := []struct {
		name  string
		value int64
	}{
		{name: "0 unlimited", value: 0},
		{name: "1", value: 1},
		{name: "100 (functional AC)", value: 100},
		{name: "1048576 (1 MB)", value: 1048576},
		{name: "10 MB default-sized value", value: 10 * 1024 * 1024},
		{name: "accepted max", value: MaxMaxResponseSize},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateMaxResponseSize(tc.value); err != nil {
				t.Fatalf("ValidateMaxResponseSize(%d) = %v, want nil", tc.value, err)
			}
		})
	}
}

func TestValidateMaxResponseSizeRejected(t *testing.T) {
	cases := []struct {
		name  string
		value int64
		why   string
	}{
		{name: "negative", value: -1, why: "negative value -1"},
		{name: "large negative", value: -100, why: "negative value -100"},
		{name: "rejected at max+1 (math.MaxInt64)", value: math.MaxInt64,
			why: fmt.Sprintf("value %d exceeds maximum %d", int64(math.MaxInt64), MaxMaxResponseSize)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMaxResponseSize(tc.value)
			if err == nil {
				t.Fatalf("ValidateMaxResponseSize(%d) = nil, want *InvalidMaxResponseSizeError", tc.value)
			}
			var ierr *InvalidMaxResponseSizeError
			if !errors.As(err, &ierr) {
				t.Fatalf("error type = %T, want *InvalidMaxResponseSizeError", err)
			}
			if ierr.Input != fmt.Sprintf("%d", tc.value) {
				t.Errorf("InvalidMaxResponseSizeError.Input = %q, want %q", ierr.Input, fmt.Sprintf("%d", tc.value))
			}
			if ierr.Why != tc.why {
				t.Errorf("InvalidMaxResponseSizeError.Why = %q, want %q", ierr.Why, tc.why)
			}
		})
	}
}
