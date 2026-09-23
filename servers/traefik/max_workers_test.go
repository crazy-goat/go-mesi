package traefik

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Boundary classes for maxWorkers: the zero/library-default edge, a
// minimum positive cap, a typical value, and the transport-derived maximum.
func TestNewMaxWorkersAcceptedValues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cases := []struct {
		name  string
		value int
		want  int
	}{
		{name: "zero_library_default", value: 0, want: 0},
		{name: "one_worker", value: 1, want: 1},
		{name: "typical_8", value: 8, want: 8},
		{name: "accepted_max_999999999", value: 999999999, want: 999999999},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := New(context.Background(), handler, &Config{MaxWorkers: tc.value}, "test")
			if err != nil {
				t.Fatalf("New with maxWorkers %d: %v", tc.value, err)
			}
			if got := p.(*ResponsePlugin).maxWorkers; got != tc.want {
				t.Errorf("maxWorkers %d resolved to %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestNewMaxWorkersRejectedValues(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cases := []struct {
		name  string
		value int
	}{
		{name: "negative_one", value: -1},
		{name: "negative_five", value: -5},
		{name: "rejected_max_plus_one_1000000000", value: 1000000000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(context.Background(), handler, &Config{MaxWorkers: tc.value}, "test")
			if err == nil {
				t.Fatalf("expected error for maxWorkers %d", tc.value)
			}
			if !strings.Contains(err.Error(), "maxWorkers") {
				t.Errorf("error must name maxWorkers, got %v", err)
			}
		})
	}
}

func TestMaxWorkersDecodeRejectsOverflowAndNonIntegers(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{name: "overflow_20_digits", value: "99999999999999999999"},
		{name: "decimal", value: "1.5"},
		{name: "not_a_number", value: `"abc"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			err := json.Unmarshal([]byte(`{"maxWorkers":`+tc.value+`}`), &cfg)
			if err == nil {
				t.Fatalf("expected decode error for maxWorkers %s, got %d", tc.value, cfg.MaxWorkers)
			}
			if !strings.Contains(err.Error(), "maxWorkers") {
				t.Errorf("decode error must name maxWorkers, got %v", err)
			}
		})
	}
}

func TestMaxWorkersAbsentUsesLibraryDefault(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	cfg := CreateConfig()
	if cfg.MaxWorkers != 0 {
		t.Fatalf("CreateConfig MaxWorkers = %d, want zero (library default)", cfg.MaxWorkers)
	}
	p, err := New(context.Background(), handler, cfg, "test")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.(*ResponsePlugin).maxWorkers; got != 0 {
		t.Errorf("absent maxWorkers resolved to %d, want 0 (core library default)", got)
	}
	p2, err := New(context.Background(), handler, &Config{}, "test")
	if err != nil {
		t.Fatalf("New with zero Config: %v", err)
	}
	if got := p2.(*ResponsePlugin).maxWorkers; got != 0 {
		t.Errorf("zero-value maxWorkers resolved to %d, want 0 (core library default)", got)
	}
}
