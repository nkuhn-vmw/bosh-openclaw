package security

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateGatewayToken_HasCorrectPrefix(t *testing.T) {
	token := GenerateGatewayToken()
	if !strings.HasPrefix(token, "oc_tok_") {
		t.Errorf("GenerateGatewayToken() = %q, want prefix %q", token, "oc_tok_")
	}
}

func TestGenerateGatewayToken_HasCorrectLength(t *testing.T) {
	token := GenerateGatewayToken()
	// 32 bytes in base64url no-padding = 43 characters, plus "oc_tok_" prefix (7 chars) = 50
	payload := strings.TrimPrefix(token, "oc_tok_")
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(payload)
	if err != nil {
		t.Fatalf("Failed to decode base64 payload: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("Decoded payload length = %d, want 32", len(decoded))
	}
}

func TestGenerateGatewayToken_IsUnique(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token := GenerateGatewayToken()
		if tokens[token] {
			t.Fatalf("Duplicate token generated on iteration %d: %s", i, token)
		}
		tokens[token] = true
	}
}

func TestGenerateGatewayToken_IsValidBase64URL(t *testing.T) {
	token := GenerateGatewayToken()
	payload := strings.TrimPrefix(token, "oc_tok_")
	_, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(payload)
	if err != nil {
		t.Errorf("Token payload is not valid base64url: %v", err)
	}
}

func TestGenerateNodeSeed_HasCorrectPrefix(t *testing.T) {
	seed := GenerateNodeSeed()
	if !strings.HasPrefix(seed, "seed_") {
		t.Errorf("GenerateNodeSeed() = %q, want prefix %q", seed, "seed_")
	}
}

func TestGenerateNodeSeed_HasCorrectLength(t *testing.T) {
	seed := GenerateNodeSeed()
	payload := strings.TrimPrefix(seed, "seed_")
	decoded, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(payload)
	if err != nil {
		t.Fatalf("Failed to decode base64 payload: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("Decoded payload length = %d, want 32", len(decoded))
	}
}

func TestGenerateNodeSeed_IsUnique(t *testing.T) {
	seeds := make(map[string]bool)
	for i := 0; i < 100; i++ {
		seed := GenerateNodeSeed()
		if seeds[seed] {
			t.Fatalf("Duplicate seed generated on iteration %d: %s", i, seed)
		}
		seeds[seed] = true
	}
}

func TestGenerateNodeSeed_IsValidBase64URL(t *testing.T) {
	seed := GenerateNodeSeed()
	payload := strings.TrimPrefix(seed, "seed_")
	_, err := base64.URLEncoding.WithPadding(base64.NoPadding).DecodeString(payload)
	if err != nil {
		t.Errorf("Seed payload is not valid base64url: %v", err)
	}
}

func TestGenerateGatewayToken_NoPaddingCharacters(t *testing.T) {
	token := GenerateGatewayToken()
	if strings.Contains(token, "=") {
		t.Errorf("Token should not contain padding characters, got: %s", token)
	}
}

func TestGenerateNodeSeed_NoPaddingCharacters(t *testing.T) {
	seed := GenerateNodeSeed()
	if strings.Contains(seed, "=") {
		t.Errorf("Seed should not contain padding characters, got: %s", seed)
	}
}
