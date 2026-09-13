package config

import (
	"errors"
	"fmt"
	"testing"

	"github.com/crazy-goat/go-mesi/mesi"
)

func TestValidateMaxDepthAccepted(t *testing.T) {
	cases := []struct {
		name  string
		value int
	}{
		{name: "0 passthrough", value: 0},
		{name: "1", value: 1},
		{name: "5", value: 5},
		{name: "accepted max", value: int(mesi.MaxMaxDepth)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateMaxDepth(tc.value); err != nil {
				t.Fatalf("ValidateMaxDepth(%d) = %v, want nil", tc.value, err)
			}
		})
	}
}

func TestValidateMaxDepthRejected(t *testing.T) {
	cases := []struct {
		name  string
		value int
		why   string
	}{
		{name: "negative", value: -1, why: "negative value -1"},
		{name: "large negative", value: -100, why: "negative value -100"},
		{name: "rejected at max+1", value: int(mesi.MaxMaxDepth) + 1, why: fmt.Sprintf("value %d exceeds maximum %d", mesi.MaxMaxDepth+1, mesi.MaxMaxDepth)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMaxDepth(tc.value)
			if err == nil {
				t.Fatalf("ValidateMaxDepth(%d) = nil, want *ErrInvalidMaxDepth", tc.value)
			}
			var ierr *mesi.ErrInvalidMaxDepth
			if !errors.As(err, &ierr) {
				t.Fatalf("error type = %T, want *ErrInvalidMaxDepth", err)
			}
			if ierr.Input != fmt.Sprintf("%d", tc.value) {
				t.Errorf("ErrInvalidMaxDepth.Input = %q, want %q", ierr.Input, fmt.Sprintf("%d", tc.value))
			}
			if ierr.Why != tc.why {
				t.Errorf("ErrInvalidMaxDepth.Why = %q, want %q", ierr.Why, tc.why)
			}
		})
	}
}
