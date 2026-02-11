package security

import "fmt"

const MinSafeVersion = "2026.1.29"

func ValidateVersion(version string) error {
	if version < MinSafeVersion {
		return fmt.Errorf("OpenClaw version %s is below minimum safe version %s (CVE-2026-25253)", version, MinSafeVersion)
	}
	return nil
}
