package security

import (
	"testing"
)

func TestValidateVersion_AcceptsMinimumVersion(t *testing.T) {
	err := ValidateVersion("2026.1.29", "")
	if err != nil {
		t.Errorf("ValidateVersion(%q) returned error: %v, want nil", "2026.1.29", err)
	}
}

func TestValidateVersion_AcceptsNewerVersion(t *testing.T) {
	versions := []string{
		"2026.1.30",
		"2026.2.1",
		"2026.2.10",
		"2027.1.1",
	}
	for _, v := range versions {
		err := ValidateVersion(v, "")
		if err != nil {
			t.Errorf("ValidateVersion(%q) returned error: %v, want nil", v, err)
		}
	}
}

func TestValidateVersion_RejectsOlderVersion(t *testing.T) {
	versions := []string{
		"2026.1.28",
		"2026.1.1",
		"2025.12.31",
		"2025.1.1",
	}
	for _, v := range versions {
		err := ValidateVersion(v, "")
		if err == nil {
			t.Errorf("ValidateVersion(%q) returned nil, want error", v)
		}
	}
}

func TestValidateVersion_HandlesMultiDigitMonths(t *testing.T) {
	accepted := []string{"2026.10.1", "2026.11.15", "2026.12.31"}
	for _, v := range accepted {
		if err := ValidateVersion(v, ""); err != nil {
			t.Errorf("ValidateVersion(%q) returned error: %v, want nil", v, err)
		}
	}
}

func TestValidateVersion_RejectsInvalidFormat(t *testing.T) {
	invalid := []string{"2026", "2026.1", "abc.1.2", "2026.x.1"}
	for _, v := range invalid {
		if err := ValidateVersion(v, ""); err == nil {
			t.Errorf("ValidateVersion(%q) returned nil, want error for invalid format", v)
		}
	}
}

func TestValidateVersion_RejectsEmptyVersion(t *testing.T) {
	err := ValidateVersion("", "")
	if err == nil {
		t.Error("ValidateVersion(\"\") returned nil, want error")
	}
}

func TestValidateVersion_ErrorMessageContainsCVE(t *testing.T) {
	err := ValidateVersion("2025.1.1", "")
	if err == nil {
		t.Fatal("ValidateVersion returned nil, want error")
	}
	msg := err.Error()
	if !contains(msg, "CVE-2026-25253") {
		t.Errorf("Error message %q should contain CVE identifier", msg)
	}
}

func TestValidateVersion_EmptyErrorMessageContainsCVE(t *testing.T) {
	err := ValidateVersion("", "")
	if err == nil {
		t.Fatal("ValidateVersion returned nil, want error")
	}
	msg := err.Error()
	if !contains(msg, "CVE-2026-25253") {
		t.Errorf("Error message %q should contain CVE identifier", msg)
	}
}

func TestValidateVersion_UsesCustomMinVersion(t *testing.T) {
	// Should accept 2026.2.1 when min is 2026.2.1
	if err := ValidateVersion("2026.2.1", "2026.2.1"); err != nil {
		t.Errorf("ValidateVersion with custom min returned error: %v", err)
	}
	// Should reject 2026.1.29 when min is 2026.2.1
	if err := ValidateVersion("2026.1.29", "2026.2.1"); err == nil {
		t.Error("ValidateVersion should reject version below custom minimum")
	}
	// Should accept 2026.3.1 when min is 2026.2.1
	if err := ValidateVersion("2026.3.1", "2026.2.1"); err != nil {
		t.Errorf("ValidateVersion should accept version above custom minimum: %v", err)
	}
}

func TestValidateWebSocketOrigin_MatchingHost(t *testing.T) {
	err := ValidateWebSocketOrigin("https://myapp.example.com", "myapp.example.com")
	if err != nil {
		t.Errorf("ValidateWebSocketOrigin() returned error: %v, want nil", err)
	}
}

func TestValidateWebSocketOrigin_MatchingHostCaseInsensitive(t *testing.T) {
	err := ValidateWebSocketOrigin("https://MyApp.Example.COM", "myapp.example.com")
	if err != nil {
		t.Errorf("ValidateWebSocketOrigin() returned error: %v, want nil", err)
	}
}

func TestValidateWebSocketOrigin_MismatchedHost(t *testing.T) {
	err := ValidateWebSocketOrigin("https://evil.attacker.com", "myapp.example.com")
	if err == nil {
		t.Error("ValidateWebSocketOrigin() returned nil for mismatched host, want error")
	}
}

func TestValidateWebSocketOrigin_EmptyOrigin(t *testing.T) {
	err := ValidateWebSocketOrigin("", "myapp.example.com")
	if err == nil {
		t.Error("ValidateWebSocketOrigin() returned nil for empty origin, want error")
	}
}

func TestValidateWebSocketOrigin_WithPort(t *testing.T) {
	err := ValidateWebSocketOrigin("https://myapp.example.com:8443", "myapp.example.com")
	if err != nil {
		t.Errorf("ValidateWebSocketOrigin() returned error: %v, want nil", err)
	}
}

func TestValidateWebSocketOrigin_HTTPScheme(t *testing.T) {
	err := ValidateWebSocketOrigin("http://myapp.example.com", "myapp.example.com")
	if err != nil {
		t.Errorf("ValidateWebSocketOrigin() returned error: %v, want nil", err)
	}
}

func TestValidateWebSocketOrigin_WSSScheme(t *testing.T) {
	err := ValidateWebSocketOrigin("wss://myapp.example.com", "myapp.example.com")
	if err != nil {
		t.Errorf("ValidateWebSocketOrigin() returned error: %v, want nil", err)
	}
}

func TestValidateWebSocketOrigin_SubdomainMismatch(t *testing.T) {
	err := ValidateWebSocketOrigin("https://sub.myapp.example.com", "myapp.example.com")
	if err == nil {
		t.Error("ValidateWebSocketOrigin() should reject subdomain mismatch")
	}
}

func TestDefaultSecurityPolicy_ControlUIDisabled(t *testing.T) {
	policy := DefaultSecurityPolicy()
	if policy.ControlUIEnabled {
		t.Error("DefaultSecurityPolicy().ControlUIEnabled = true, want false")
	}
}

func TestDefaultSecurityPolicy_RequireAuth(t *testing.T) {
	policy := DefaultSecurityPolicy()
	if !policy.ControlUIRequireAuth {
		t.Error("DefaultSecurityPolicy().ControlUIRequireAuth = false, want true")
	}
}

func TestDefaultSecurityPolicy_WebSocketOriginCheck(t *testing.T) {
	policy := DefaultSecurityPolicy()
	if !policy.WebSocketOriginCheck {
		t.Error("DefaultSecurityPolicy().WebSocketOriginCheck = false, want true")
	}
}

func TestDefaultSecurityPolicy_MinVersion(t *testing.T) {
	policy := DefaultSecurityPolicy()
	if policy.MinVersion != MinSafeVersion {
		t.Errorf("DefaultSecurityPolicy().MinVersion = %q, want %q", policy.MinVersion, MinSafeVersion)
	}
}

func TestMinSafeVersion_Value(t *testing.T) {
	if MinSafeVersion != "2026.1.29" {
		t.Errorf("MinSafeVersion = %q, want %q", MinSafeVersion, "2026.1.29")
	}
}

// helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
