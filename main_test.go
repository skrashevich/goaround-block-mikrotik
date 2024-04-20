package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zalando/go-keyring"
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

func TestSaveAndGetCreds(t *testing.T) {
	service := "TestService"
	user := "TestUser"
	password := "TestPassword"

	// Attempt to save credentials.
	err := saveCreds(service, user, password)
	if err != nil {
		t.Errorf("Failed to save credentials: %v", err)
	}

	// Attempt to retrieve the saved credentials.
	retrievedPassword, err := getCreds(service, user)
	if err != nil {
		t.Errorf("Failed to retrieve credentials: %v", err)
	}

	// Verify the retrieved credentials match what was saved.
	if retrievedPassword != password {
		t.Errorf("Retrieved password does not match saved password. Got %s, want %s", retrievedPassword, password)
	}

	// Cleanup: Remove the test credentials from the keyring to avoid pollution.
	// Note: This cleanup step is crucial to prevent leaving test data in the system's keyring.
	err = keyring.Delete(service, user)
	if err != nil {
		t.Logf("Warning: Failed to clean up test credentials from keyring: %v", err)
	}
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

			_, _, _, _, _, _, _, _, _, err := parseFlags()

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
