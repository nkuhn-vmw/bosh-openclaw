package security

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

func GenerateGatewayToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return "oc_tok_" + base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
}

func GenerateNodeSeed() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return "seed_" + base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(b)
}
