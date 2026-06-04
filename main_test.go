package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	koanf "github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"github.com/zalando/go-keyring"
)

func TestResolveDomain(t *testing.T) {
	// Define test cases
	tests := []struct {
		domain  string
		wantErr bool
	}{
		{"localhost", false},                  // localhost always resolves
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

func TestSaveAndGetCreds(t *testing.T) {
	service := "TestService"
	user := "TestUser"
	password := "TestPassword"

	// Detect whether keyring works in this environment by attempting to set a credential
	if err := keyring.Set(service, user, password); err != nil {
		t.Skipf("keyring not available in this environment: %v", err)
	}

	retrievedPassword, err := keyring.Get(service, user)
	if err != nil {
		t.Skipf("keyring not available in this environment: %v", err)
	}

	// Verify the retrieved credentials match what was saved.
	if retrievedPassword != password {
		t.Fatalf("Retrieved password does not match saved password. Got %q, want %q", retrievedPassword, password)
	}

	// Cleanup: Remove the test credentials from the keyring to avoid pollution.
	_ = keyring.Delete(service, user)
}

func TestParseFlags(t *testing.T) {
	keyring.Delete("127.0.0.1", "")
	keyring.Delete("127.0.0.1", "testuser")

	tests := []struct {
		name        string
		setupArgs   func()
		expectError bool
		ErrorString string
	}{
		{
			name: "Missing required flags",
			setupArgs: func() {
				os.Args = []string{"cmd", "--username=testuser"}
			},
			ErrorString: "Missing required parameters: domain, address, password, gateway\n",
			expectError: true,
		},
		{
			name: "Valid credentials",
			setupArgs: func() {
				os.Args = []string{"cmd", "--address=127.0.0.1", "--username=testuser", "--password=testpass", "--gateway=1.1.1.1", "--domain=google.com"}
			},
			expectError: false,
			ErrorString: "",
		},
		{
			name: "Empty username",
			setupArgs: func() {
				os.Args = []string{"cmd", "--address=127.0.0.1", "--username=", "--password=testpass", "--gateway=1.1.1.1", "--domain=google.com"}
			},
			ErrorString: "Missing required parameters: username\n",
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {

			// Reset flag.CommandLine between tests to avoid flag redefinition errors.
			flag.CommandLine = flag.NewFlagSet("", flag.PanicOnError)

			tc.setupArgs()

			k := koanf.New(".")
			_, err := parseFlags(k)

			if tc.expectError {
				assert.Error(t, err)
				assert.EqualError(t, err, tc.ErrorString)
			} else {
				assert.NoError(t, err)
			}
		})
	}

}

func TestGetConfigFile(t *testing.T) {
	expectedConfigDirSuffix := filepath.Join("go-mikrotik-block", "config.yaml")

	configFile, err := getConfigFile()
	if err != nil {
		t.Fatalf("getConfigFile() returned an error: %v", err)
	}

	if !strings.HasSuffix(configFile, expectedConfigDirSuffix) {
		t.Errorf("getConfigFile() = %v, want suffix %v", configFile, expectedConfigDirSuffix)
	}
}
