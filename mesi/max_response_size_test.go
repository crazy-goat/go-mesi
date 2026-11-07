package mesi

import (
	"errors"
	"math"
	"strings"
	"testing"
)

func TestMaxMaxResponseSizeConstant(t *testing.T) {
	if MaxMaxResponseSize != math.MaxInt64-1 {
		t.Errorf("MaxMaxResponseSize = %d, want %d", MaxMaxResponseSize, int64(math.MaxInt64)-1)
	}
	// The whole point of the bound: MaxResponseSize+1 must not wrap.
	if MaxMaxResponseSize+1 != math.MaxInt64 {
		t.Errorf("MaxMaxResponseSize+1 = %d, want %d", MaxMaxResponseSize+1, int64(math.MaxInt64))
	}
}

func TestValidateMaxResponseSize(t *testing.T) {
	tests := []struct {
		name string
		size int64
	}{
		{name: "zero_unlimited", size: 0},
		{name: "small", size: 1},
		{name: "ordinary", size: 10 * 1024 * 1024},
		{name: "accepted_max", size: MaxMaxResponseSize},
		{name: "negative_kept_as_unlimited_legacy", size: -1},
		{name: "negative_min_int64_kept_as_unlimited_legacy", size: math.MinInt64},
		{name: "rejected_at_max_plus_one", size: MaxMaxResponseSize + 1},
		{name: "rejected_max_int64", size: math.MaxInt64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMaxResponseSize(tc.size)
			if tc.size > MaxMaxResponseSize {
				if err == nil {
					t.Fatalf("validateMaxResponseSize(%d) = nil, want an error", tc.size)
				}
				var typed *ErrInvalidMaxResponseSize
				if !errors.As(err, &typed) {
					t.Fatalf("validateMaxResponseSize(%d) error = %T (%v), want *ErrInvalidMaxResponseSize", tc.size, err, err)
				}
				if typed.Size != tc.size {
					t.Errorf("error Size = %d, want %d", typed.Size, tc.size)
				}
				if !strings.Contains(err.Error(), "invalid MaxResponseSize") {
					t.Errorf("error message %q does not name the field", err.Error())
				}
				if !strings.Contains(err.Error(), "9223372036854775806") {
					t.Errorf("error message %q does not name the accepted maximum", err.Error())
				}
				return
			}
			if err != nil {
				t.Errorf("validateMaxResponseSize(%d) = %v, want nil", tc.size, err)
			}
		})
	}
}

func TestErrInvalidMaxResponseSizeMessage(t *testing.T) {
	err := &ErrInvalidMaxResponseSize{Size: math.MaxInt64, Why: "value must be at most 9223372036854775806"}
	want := "invalid MaxResponseSize 9223372036854775807: value must be at most 9223372036854775806"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}
