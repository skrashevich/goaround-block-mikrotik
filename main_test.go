package main

import (
	"testing"
)

func TestResolveDomain(t *testing.T) {
	// Define test cases
	tests := []struct {
		domain  string
		wantErr bool
	}{
		{"google.com", false},                // Assuming google.com will always resolve
		{"invalid-domain-name.likely", true}, // An invalid domain name should result in an error
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got, err := resolveDomain(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveDomain() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(got) == 0 {
				t.Errorf("Expected at least one IP address for %s, got none", tt.domain)
			}
		})
	}
}

// TestSanitizeDomain tests the sanitizeDomain function for various cases.
func TestSanitizeDomain(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		want   string
	}{
		{
			name:   "ContainsEqual",
			domain: "example.com?key=value",
			want:   "example.com?key\\=value",
		},
		{
			name:   "NoSpecialChars",
			domain: "example.com",
			want:   "example.com",
		},
		{
			name:   "MultipleEquals",
			domain: "example.com?one=1&two=2",
			want:   "example.com?one\\=1&two\\=2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeDomain(tt.domain); got != tt.want {
				t.Errorf("sanitizeDomain(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}
