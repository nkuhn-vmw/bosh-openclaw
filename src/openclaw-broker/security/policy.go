package security

import (
	"fmt"
	"net/url"
	"strings"
)

const MinSafeVersion = "2026.1.29"

// ValidateVersion ensures the OpenClaw version meets the minimum safe version
// required to mitigate CVE-2026-25253 (1-click RCE via WebSocket token exfiltration).
func ValidateVersion(version string) error {
	if version == "" {
		return fmt.Errorf("OpenClaw version is required (minimum: %s for CVE-2026-25253)", MinSafeVersion)
	}
	if version < MinSafeVersion {
		return fmt.Errorf("OpenClaw version %s is below minimum safe version %s (CVE-2026-25253)", version, MinSafeVersion)
	}
	return nil
}

// ValidateWebSocketOrigin checks that a WebSocket connection's Origin header
// matches the expected hostname, preventing cross-origin token exfiltration.
func ValidateWebSocketOrigin(origin, expectedHost string) error {
	if origin == "" {
		return fmt.Errorf("WebSocket origin header is required")
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("invalid origin URL: %w", err)
	}
	if !strings.EqualFold(parsed.Hostname(), expectedHost) {
		return fmt.Errorf("origin %q does not match expected host %q", origin, expectedHost)
	}
	return nil
}

// SecurityPolicy represents the runtime security policy for an agent instance.
type SecurityPolicy struct {
	ControlUIEnabled     bool   `json:"control_ui_enabled"`
	ControlUIRequireAuth bool   `json:"control_ui_require_auth"`
	WebSocketOriginCheck bool   `json:"websocket_origin_check"`
	MinVersion           string `json:"min_version"`
}

// DefaultSecurityPolicy returns a policy with CVE-2026-25253 mitigations active.
func DefaultSecurityPolicy() SecurityPolicy {
	return SecurityPolicy{
		ControlUIEnabled:     false,
		ControlUIRequireAuth: true,
		WebSocketOriginCheck: true,
		MinVersion:           MinSafeVersion,
	}
}
