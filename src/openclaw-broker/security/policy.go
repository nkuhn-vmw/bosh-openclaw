package security

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const MinSafeVersion = "2026.1.29"

// parseVersion splits a "YYYY.M.D" or "YYYY.M.D-N" version string into integer components.
// Any hyphen suffix (e.g., "-2" in "2026.2.21-2") is stripped from the day component.
func parseVersion(v string) (int, int, int, error) {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid version format %q (expected YYYY.M.D)", v)
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid year in version %q: %w", v, err)
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid month in version %q: %w", v, err)
	}
	dayStr, _, _ := strings.Cut(parts[2], "-")
	day, err := strconv.Atoi(dayStr)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid day in version %q: %w", v, err)
	}
	return year, month, day, nil
}

// ValidateVersion ensures the OpenClaw version meets the given minimum version.
// If minVersion is empty, falls back to the hardcoded MinSafeVersion constant.
func ValidateVersion(version, minVersion string) error {
	if minVersion == "" {
		minVersion = MinSafeVersion
	}
	if version == "" {
		return fmt.Errorf("OpenClaw version is required (minimum: %s for CVE-2026-25253)", minVersion)
	}
	vY, vM, vD, err := parseVersion(version)
	if err != nil {
		return err
	}
	mY, mM, mD, err := parseVersion(minVersion)
	if err != nil {
		return fmt.Errorf("invalid minimum version %q: %w", minVersion, err)
	}

	if vY < mY || (vY == mY && vM < mM) || (vY == mY && vM == mM && vD < mD) {
		return fmt.Errorf("OpenClaw version %s is below minimum safe version %s (CVE-2026-25253)", version, minVersion)
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
	WebSocketOriginCheck bool   `json:"websocket_origin_check"`
	MinVersion           string `json:"min_version"`
}

// DefaultSecurityPolicy returns a policy with CVE-2026-25253 mitigations active.
func DefaultSecurityPolicy() SecurityPolicy {
	return SecurityPolicy{
		WebSocketOriginCheck: true,
		MinVersion:           MinSafeVersion,
	}
}
