package main

import (
	"net"
	"reflect"
	"testing"
)

// mockLookupIP is a function that matches the signature of net.LookupIP and can be used to override behavior in tests.
var mockLookupIP = net.LookupIP

func TestResolveDomain(t *testing.T) {
	// Override the net.LookupIP function with a mock function for testing.
	mockLookupIP = func(domain string) ([]net.IP, error) {
		if domain == "example.com" {
			return []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")}, nil
		}
		if domain == "nonexistent.domain" {
			return nil, &net.DNSError{Err: "no such host", IsNotFound: true}
		}
		return nil, nil
	}

	tests := []struct {
		name    string
		domain  string
		wantIPs []net.IP
		wantErr bool
	}{
		{
			name:    "valid domain",
			domain:  "example.com",
			wantIPs: []net.IP{net.ParseIP("93.184.216.34"), net.ParseIP("2606:2800:220:1:248:1893:25c8:1946")},
			wantErr: false,
		},
		{
			name:    "nonexistent domain",
			domain:  "nonexistent.domain",
			wantIPs: nil,
			wantErr: true,
		},
		{
			name:    "empty domain",
			domain:  "",
			wantIPs: nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIPs, err := resolveDomain(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveDomain() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(gotIPs, tt.wantIPs) {
				t.Errorf("resolveDomain() gotIPs = %v, want %v", gotIPs, tt.wantIPs)
			}
		})
	}

	// Reset the mockLookupIP to its original state after the tests.
	mockLookupIP = net.LookupIP
}
