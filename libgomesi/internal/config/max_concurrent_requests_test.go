package config

import (
	"errors"
	"fmt"
	"testing"
)

func TestMaxMaxConcurrentRequestsConstant(t *testing.T) {
	// The cap mirrors Apache's MESI_MAX_MAX_CONCURRENT_REQUESTS
	// (servers/apache/mod_mesi.c): the 9-digit guard of the shared
	// strict parse_nonneg_int helper, i.e. the largest value that fits
	// both the C `int` field and the helper without a wrap. Neither
	// core nor Caddy defines a maximum — pin the exact value so a
	// change is deliberate and stays in sync with the Apache-side
	// #define and the README/CHANGELOG docs.
	if MaxMaxConcurrentRequests != 999999999 {
		t.Errorf("MaxMaxConcurrentRequests = %d, want 999999999", MaxMaxConcurrentRequests)
	}
}

func TestValidateMaxConcurrentRequestsAccepted(t *testing.T) {
	cases := []struct {
		name  string
		value int
	}{
		{name: "0 unlimited", value: 0},
		{name: "1", value: 1},
		{name: "issue example 5", value: 5},
		{name: "functional cap 3", value: 3},
		{name: "1000", value: 1000},
		{name: "accepted max", value: MaxMaxConcurrentRequests},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateMaxConcurrentRequests(tc.value); err != nil {
				t.Fatalf("ValidateMaxConcurrentRequests(%d) = %v, want nil", tc.value, err)
			}
		})
	}
}

func TestValidateMaxConcurrentRequestsRejected(t *testing.T) {
	cases := []struct {
		name  string
		value int
		why   string
	}{
		{name: "negative", value: -1, why: "negative value -1"},
		{name: "large negative", value: -100, why: "negative value -100"},
		{name: "rejected at max+1", value: MaxMaxConcurrentRequests + 1,
			why: fmt.Sprintf("value %d exceeds maximum %d", MaxMaxConcurrentRequests+1, MaxMaxConcurrentRequests)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMaxConcurrentRequests(tc.value)
			if err == nil {
				t.Fatalf("ValidateMaxConcurrentRequests(%d) = nil, want *InvalidMaxConcurrentRequestsError", tc.value)
			}
			var ierr *InvalidMaxConcurrentRequestsError
			if !errors.As(err, &ierr) {
				t.Fatalf("error type = %T, want *InvalidMaxConcurrentRequestsError", err)
			}
			if want := fmt.Sprintf("%d", tc.value); ierr.Input != want {
				t.Errorf("InvalidMaxConcurrentRequestsError.Input = %q, want %q", ierr.Input, want)
			}
			if ierr.Why != tc.why {
				t.Errorf("InvalidMaxConcurrentRequestsError.Why = %q, want %q", ierr.Why, tc.why)
			}
		})
	}
}
