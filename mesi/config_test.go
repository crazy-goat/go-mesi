package mesi

import (
	"math"
	"testing"
	"time"
)

func TestCreateDefaultConfig(t *testing.T) {
	config := CreateDefaultConfig()

	if config.Context == nil {
		t.Error("Context should not be nil")
	}
	if config.DefaultUrl != "http://127.0.0.1/" {
		t.Errorf("DefaultUrl = %q, want %q", config.DefaultUrl, "http://127.0.0.1/")
	}
	if config.MaxDepth != 5 {
		t.Errorf("MaxDepth = %d, want 5", config.MaxDepth)
	}
	if config.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", config.Timeout)
	}
	if config.ParseOnHeader != false {
		t.Error("ParseOnHeader should be false")
	}
	if config.BlockPrivateIPs != true {
		t.Error("BlockPrivateIPs should be true")
	}
	if config.MaxResponseSize != 10*1024*1024 {
		t.Errorf("MaxResponseSize = %d, want 10MB", config.MaxResponseSize)
	}
	if config.CacheKeyFunc == nil {
		t.Error("CacheKeyFunc should not be nil")
	}
}

func TestCanGoDeeper(t *testing.T) {
	tests := []struct {
		name     string
		maxDepth uint
		timeout  time.Duration
		elapsed  time.Duration
		expected bool
	}{
		{"can go deeper", 5, 10 * time.Second, 2 * time.Second, true},
		{"max depth zero", 0, 10 * time.Second, 2 * time.Second, false},
		{"timeout exceeded", 5, 10 * time.Second, 15 * time.Second, false},
		{"timeout equal elapsed", 5, 10 * time.Second, 10 * time.Second, false},
		{"max depth one with time", 1, 10 * time.Second, 5 * time.Second, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{MaxDepth: tt.maxDepth, Timeout: tt.timeout}
			if got := config.CanGoDeeper(tt.elapsed); got != tt.expected {
				t.Errorf("CanGoDeeper() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseOnly(t *testing.T) {
	tests := []struct {
		name     string
		maxDepth uint
		expected bool
	}{
		{"parse only when zero", 0, true},
		{"not parse only when positive", 1, false},
		{"not parse only when five", 5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{MaxDepth: tt.maxDepth}
			if got := config.ParseOnly(); got != tt.expected {
				t.Errorf("ParseOnly() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDecreaseMaxDepth(t *testing.T) {
	tests := []struct {
		name     string
		maxDepth uint
		expected uint
	}{
		{"decrease from five", 5, 4},
		{"decrease from one", 1, 0},
		{"stay at zero", 0, 0},
		{"decrease from max uint", math.MaxUint, math.MaxUint - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{MaxDepth: tt.maxDepth}
			result := config.DecreaseMaxDepth()
			if result.MaxDepth != tt.expected {
				t.Errorf("DecreaseMaxDepth() MaxDepth = %d, want %d", result.MaxDepth, tt.expected)
			}
		})
	}
}

func TestWithElapsedTime(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		elapsed  time.Duration
		expected time.Duration
	}{
		{"subtract elapsed", 10 * time.Second, 3 * time.Second, 7 * time.Second},
		{"elapsed equals timeout", 10 * time.Second, 10 * time.Second, 0},
		{"elapsed exceeds timeout", 10 * time.Second, 15 * time.Second, 0},
		{"no elapsed time", 10 * time.Second, 0, 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{Timeout: tt.timeout}
			result := config.WithElapsedTime(tt.elapsed)
			if result.Timeout != tt.expected {
				t.Errorf("WithElapsedTime() Timeout = %v, want %v", result.Timeout, tt.expected)
			}
		})
	}
}

func TestOverrideConfigWithTimeout(t *testing.T) {
	tests := []struct {
		name         string
		configTTL    time.Duration
		tokenTimeout string
		expected     time.Duration
	}{
		{"token timeout smaller", 10 * time.Second, "5", 5 * time.Second},
		{"token timeout larger", 5 * time.Second, "10", 5 * time.Second},
		{"invalid timeout", 10 * time.Second, "invalid", 10 * time.Second},
		{"empty timeout", 10 * time.Second, "", 10 * time.Second},
		{"zero timeout", 10 * time.Second, "0", 10 * time.Second},
		{"negative timeout", 10 * time.Second, "-1", 10 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{Timeout: tt.configTTL}
			token := esiIncludeToken{Timeout: tt.tokenTimeout}
			result := config.OverrideConfig(token)
			if result.Timeout != tt.expected {
				t.Errorf("OverrideConfig() Timeout = %v, want %v", result.Timeout, tt.expected)
			}
		})
	}
}

func TestOverrideConfigWithMaxDepth(t *testing.T) {
	tests := []struct {
		name          string
		configDepth   uint
		tokenMaxDepth string
		expected      uint
	}{
		{"token limit lower than config", 10, "3", 4},
		{"token limit higher than config", 5, "10", 5},
		{"invalid max depth", 10, "invalid", 10},
		{"empty max depth", 10, "", 10},
		{"zero max depth becomes limit 1", 10, "0", 1},
		{"negative max depth ignored", 10, "-1", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := EsiParserConfig{MaxDepth: tt.configDepth}
			token := esiIncludeToken{MaxDepth: tt.tokenMaxDepth}
			result := config.OverrideConfig(token)
			if result.MaxDepth != tt.expected {
				t.Errorf("OverrideConfig() MaxDepth = %d, want %d", result.MaxDepth, tt.expected)
			}
		})
	}
}

func TestOverrideConfigWithBothTimeoutAndMaxDepth(t *testing.T) {
	config := EsiParserConfig{
		Timeout:  10 * time.Second,
		MaxDepth: 10,
	}
	token := esiIncludeToken{
		Timeout:  "3",
		MaxDepth: "2",
	}
	result := config.OverrideConfig(token)

	if result.Timeout != 3*time.Second {
		t.Errorf("Timeout = %v, want 3s", result.Timeout)
	}
	if result.MaxDepth != 3 {
		t.Errorf("MaxDepth = %d, want 3", result.MaxDepth)
	}
}
