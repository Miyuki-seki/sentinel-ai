package main

import (
	"os"
	"reflect"
	"testing"
)

func TestParseTargetURLs(t *testing.T) {
	tests := []struct {
		name       string
		targetURLs string
		targetURL  string
		expected   []string
	}{
		{
			name:       "default fallback",
			targetURLs: "",
			targetURL:  "",
			expected:   []string{"http://localhost:8081/health"},
		},
		{
			name:       "fallback to TARGET_URL",
			targetURLs: "",
			targetURL:  "http://example.com/health",
			expected:   []string{"http://example.com/health"},
		},
		{
			name:       "multiple urls via TARGET_URLS",
			targetURLs: "http://target1:8081/health, http://target2:8082/health ,http://target3:8083/health",
			targetURL:  "",
			expected: []string{
				"http://target1:8081/health",
				"http://target2:8082/health",
				"http://target3:8083/health",
			},
		},
		{
			name:       "TARGET_URLS takes precedence over TARGET_URL",
			targetURLs: "http://primary:8081/health",
			targetURL:  "http://secondary:8082/health",
			expected:   []string{"http://primary:8081/health"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.targetURLs != "" {
				os.Setenv("TARGET_URLS", tt.targetURLs)
			} else {
				os.Unsetenv("TARGET_URLS")
			}
			if tt.targetURL != "" {
				os.Setenv("TARGET_URL", tt.targetURL)
			} else {
				os.Unsetenv("TARGET_URL")
			}

			actual := parseTargetURLs()
			if !reflect.DeepEqual(actual, tt.expected) {
				t.Errorf("got %v, expected %v", actual, tt.expected)
			}
		})
	}
}
