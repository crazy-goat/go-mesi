package config

import (
	"errors"
	"fmt"
	"testing"
)

func TestMaxMaxWorkersConstant(t *testing.T) {
	// The cap mirrors Apache's MESI_MAX_MAX_WORKERS
	// (servers/apache/mod_mesi.c): the 9-digit guard of the shared
	// strict parse_nonneg_int helper, i.e. the largest value that fits
	// both the C `int` field and the helper without a wrap. Neither
	// core nor Caddy defines a maximum — pin the exact value so a
	// change is deliberate and stays in sync with the Apache-side
	// #define and the README/CHANGELOG docs.
	if MaxMaxWorkers != 999999999 {
		t.Errorf("MaxMaxWorkers = %d, want 999999999", MaxMaxWorkers)
	}
}

func TestValidateMaxWorkersAccepted(t *testing.T) {
	cases := []struct {
		name  string
		value int
	}{
		{name: "0 library default", value: 0},
		{name: "1 serializes token processing", value: 1},
		{name: "issue example 4", value: 4},
		{name: "issue example 8", value: 8},
		{name: "functional cap 2", value: 2},
		{name: "1000", value: 1000},
		{name: "accepted max", value: MaxMaxWorkers},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateMaxWorkers(tc.value); err != nil {
				t.Fatalf("ValidateMaxWorkers(%d) = %v, want nil", tc.value, err)
			}
		})
	}
}

func TestValidateMaxWorkersRejected(t *testing.T) {
	cases := []struct {
		name  string
		value int
		why   string
	}{
		{name: "negative", value: -1, why: "negative value -1"},
		{name: "large negative", value: -100, why: "negative value -100"},
		{name: "rejected at max+1", value: MaxMaxWorkers + 1,
			why: fmt.Sprintf("value %d exceeds maximum %d", MaxMaxWorkers+1, MaxMaxWorkers)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMaxWorkers(tc.value)
			if err == nil {
				t.Fatalf("ValidateMaxWorkers(%d) = nil, want *InvalidMaxWorkersError", tc.value)
			}
			var ierr *InvalidMaxWorkersError
			if !errors.As(err, &ierr) {
				t.Fatalf("error type = %T, want *InvalidMaxWorkersError", err)
			}
			if want := fmt.Sprintf("%d", tc.value); ierr.Input != want {
				t.Errorf("InvalidMaxWorkersError.Input = %q, want %q", ierr.Input, want)
			}
			if ierr.Why != tc.why {
				t.Errorf("InvalidMaxWorkersError.Why = %q, want %q", ierr.Why, tc.why)
			}
		})
	}
}
