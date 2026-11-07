package main

import (
	"strings"
	"testing"
)

func TestResolveBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		listen   string
		override string
		want     string
	}{
		{
			name:   "wildcard listen derives loopback",
			listen: ":8080",
			want:   "http://127.0.0.1:8080",
		},
		{
			name:   "loopback listen is kept",
			listen: "127.0.0.1:9090",
			want:   "http://127.0.0.1:9090",
		},
		{
			name:   "hostname listen is kept",
			listen: "localhost:3000",
			want:   "http://localhost:3000",
		},
		{
			name:   "unspecified IPv4 listen maps to loopback",
			listen: "0.0.0.0:8080",
			want:   "http://127.0.0.1:8080",
		},
		{
			name:   "unspecified IPv6 listen maps to IPv6 loopback",
			listen: "[::]:8080",
			want:   "http://[::1]:8080",
		},
		{
			name:   "IPv6 loopback is bracketed",
			listen: "[::1]:8080",
			want:   "http://[::1]:8080",
		},
		{
			name:     "override wins over listen",
			listen:   ":8080",
			override: "https://esi.example.test:8443",
			want:     "https://esi.example.test:8443",
		},
		{
			name:     "override trailing slash is dropped",
			listen:   ":8080",
			override: "http://127.0.0.1:9090/",
			want:     "http://127.0.0.1:9090",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveBaseURL(tt.listen, tt.override)
			if err != nil {
				t.Fatalf("resolveBaseURL(%q, %q) returned error: %v", tt.listen, tt.override, err)
			}
			if got != tt.want {
				t.Errorf("resolveBaseURL(%q, %q) = %q, want %q", tt.listen, tt.override, got, tt.want)
			}
		})
	}
}

func TestResolveBaseURLErrors(t *testing.T) {
	tests := []struct {
		name     string
		listen   string
		override string
		wantErr  string
	}{
		{
			name:    "listen without port",
			listen:  "localhost",
			wantErr: "cannot derive include base URL",
		},
		{
			name:    "empty listen",
			listen:  "",
			wantErr: "cannot derive include base URL",
		},
		{
			name:    "listen with empty port",
			listen:  "localhost:",
			wantErr: "missing port",
		},
		{
			name:     "override without scheme",
			override: "127.0.0.1:9090",
			wantErr:  "invalid -base-url",
		},
		{
			name:     "override with unsupported scheme",
			override: "ftp://127.0.0.1:9090",
			wantErr:  "scheme must be http or https",
		},
		{
			name:     "override without host",
			override: "http://",
			wantErr:  "invalid -base-url",
		},
		{
			name:     "override with path",
			override: "http://127.0.0.1:9090/prefix",
			wantErr:  "path must be empty",
		},
		{
			name:     "override with query",
			override: "http://127.0.0.1:9090/?q=1",
			wantErr:  "query and fragment are not allowed",
		},
		{
			name:     "override with fragment",
			override: "http://127.0.0.1:9090/#fragment",
			wantErr:  "query and fragment are not allowed",
		},
		{
			name:     "unparseable override",
			override: "://bad",
			wantErr:  "invalid -base-url",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveBaseURL(tt.listen, tt.override)
			if err == nil {
				t.Fatalf("resolveBaseURL(%q, %q) returned no error, want %q", tt.listen, tt.override, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("resolveBaseURL(%q, %q) error = %q, want it to contain %q", tt.listen, tt.override, err, tt.wantErr)
			}
		})
	}
}
