// Package telemetry handles OpenTelemetry setup and export.
package telemetry

import (
	"strings"
)

// extractHost extracts the host:port from a URL.
func extractHost(endpoint string) string {
	// Remove protocol
	s := endpoint
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")

	// Get host part (before first /)
	if idx := strings.Index(s, "/"); idx != -1 {
		s = s[:idx]
	}
	return s
}

// extractPath extracts the path from a URL.
func extractPath(endpoint string) string {
	// Remove protocol
	s := endpoint
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")

	// Get path part (after first /)
	if idx := strings.Index(s, "/"); idx != -1 {
		return s[idx:]
	}
	return ""
}
