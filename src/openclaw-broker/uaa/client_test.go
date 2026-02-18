package uaa

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newFakeUAA creates an httptest.Server that simulates the CF UAA API.
// adminID/adminSecret are the expected admin credentials.
// existingClients tracks registered client IDs to test idempotency.
func newFakeUAA(adminID, adminSecret string) (*httptest.Server, map[string]bool) {
	existingClients := make(map[string]bool)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// POST /oauth/token — client_credentials grant
		case r.Method == "POST" && r.URL.Path == "/oauth/token":
			if err := r.ParseForm(); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			cid := r.FormValue("client_id")
			csec := r.FormValue("client_secret")
			if cid != adminID || csec != adminSecret {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "fake-admin-token",
				"token_type":   "bearer",
				"expires_in":   3600,
			})

		// POST /oauth/clients — create client
		case r.Method == "POST" && r.URL.Path == "/oauth/clients":
			auth := r.Header.Get("Authorization")
			if auth != "Bearer fake-admin-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var client OAuthClient
			if err := json.NewDecoder(r.Body).Decode(&client); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if existingClients[client.ClientID] {
				w.WriteHeader(http.StatusConflict)
				w.Write([]byte(`{"error":"Client already exists"}`))
				return
			}
			existingClients[client.ClientID] = true
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(client)

		// DELETE /oauth/clients/{id} — delete client
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/oauth/clients/"):
			auth := r.Header.Get("Authorization")
			if auth != "Bearer fake-admin-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			clientID := strings.TrimPrefix(r.URL.Path, "/oauth/clients/")
			if !existingClients[clientID] {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(existingClients, clientID)
			w.WriteHeader(http.StatusOK)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	return server, existingClients
}

func TestGetAdminToken_Success(t *testing.T) {
	server, _ := newFakeUAA("admin", "admin-secret")
	defer server.Close()

	client := NewClient(server.URL, "admin", "admin-secret", false)
	token, err := client.getAdminToken()
	if err != nil {
		t.Fatalf("getAdminToken failed: %v", err)
	}
	if token != "fake-admin-token" {
		t.Errorf("token = %q, want %q", token, "fake-admin-token")
	}
}

func TestGetAdminToken_BadCredentials(t *testing.T) {
	server, _ := newFakeUAA("admin", "admin-secret")
	defer server.Close()

	client := NewClient(server.URL, "admin", "wrong-secret", false)
	_, err := client.getAdminToken()
	if err == nil {
		t.Fatal("Expected error for bad credentials")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("Error = %q, should contain 'status 401'", err.Error())
	}
}

func TestCreateClient_Success(t *testing.T) {
	server, existing := newFakeUAA("admin", "secret")
	defer server.Close()

	client := NewClient(server.URL, "admin", "secret", false)
	err := client.CreateClient(OAuthClient{
		ClientID:             "openclaw-test-001",
		ClientSecret:         "test-secret",
		AuthorizedGrantTypes: []string{"authorization_code"},
		RedirectURI:          []string{"https://test.apps.example.com/oauth2/callback"},
		Scope:                []string{"openid"},
	})
	if err != nil {
		t.Fatalf("CreateClient failed: %v", err)
	}
	if !existing["openclaw-test-001"] {
		t.Error("Client should be registered in UAA")
	}
}

func TestCreateClient_Idempotent(t *testing.T) {
	server, _ := newFakeUAA("admin", "secret")
	defer server.Close()

	client := NewClient(server.URL, "admin", "secret", false)
	oauthClient := OAuthClient{
		ClientID:             "openclaw-dup",
		ClientSecret:         "dup-secret",
		AuthorizedGrantTypes: []string{"authorization_code"},
		RedirectURI:          []string{"https://dup.apps.example.com/oauth2/callback"},
		Scope:                []string{"openid"},
	}

	// First create
	if err := client.CreateClient(oauthClient); err != nil {
		t.Fatalf("First CreateClient failed: %v", err)
	}

	// Second create (should succeed — idempotent)
	if err := client.CreateClient(oauthClient); err != nil {
		t.Fatalf("Second CreateClient should be idempotent, got: %v", err)
	}
}

func TestDeleteClient_Success(t *testing.T) {
	server, existing := newFakeUAA("admin", "secret")
	defer server.Close()

	client := NewClient(server.URL, "admin", "secret", false)

	// Create first
	existing["openclaw-del-001"] = true

	// Delete
	if err := client.DeleteClient("openclaw-del-001"); err != nil {
		t.Fatalf("DeleteClient failed: %v", err)
	}
	if existing["openclaw-del-001"] {
		t.Error("Client should be removed from UAA")
	}
}

func TestDeleteClient_NotFound_Idempotent(t *testing.T) {
	server, _ := newFakeUAA("admin", "secret")
	defer server.Close()

	client := NewClient(server.URL, "admin", "secret", false)

	// Delete non-existent client — should succeed (idempotent)
	if err := client.DeleteClient("openclaw-nonexistent"); err != nil {
		t.Fatalf("DeleteClient for non-existent should be idempotent, got: %v", err)
	}
}

func TestGenerateClientSecret_Length(t *testing.T) {
	secret := GenerateClientSecret()
	// 32 bytes = 64 hex chars
	if len(secret) != 64 {
		t.Errorf("GenerateClientSecret length = %d, want 64", len(secret))
	}
}

func TestGenerateClientSecret_Unique(t *testing.T) {
	s1 := GenerateClientSecret()
	s2 := GenerateClientSecret()
	if s1 == s2 {
		t.Error("Two GenerateClientSecret calls should produce different values")
	}
}

func TestClientIDForInstance(t *testing.T) {
	id := ClientIDForInstance("abc-123")
	if id != "openclaw-abc-123" {
		t.Errorf("ClientIDForInstance = %q, want %q", id, "openclaw-abc-123")
	}
}

func TestCreateDeleteLifecycle(t *testing.T) {
	server, existing := newFakeUAA("admin", "secret")
	defer server.Close()

	client := NewClient(server.URL, "admin", "secret", false)

	instanceID := "lifecycle-test"
	clientID := ClientIDForInstance(instanceID)
	clientSecret := GenerateClientSecret()

	// Create
	err := client.CreateClient(OAuthClient{
		ClientID:             clientID,
		ClientSecret:         clientSecret,
		AuthorizedGrantTypes: []string{"authorization_code"},
		RedirectURI:          []string{"https://oc-user-" + instanceID + ".apps.example.com/oauth2/callback"},
		Scope:                []string{"openid"},
	})
	if err != nil {
		t.Fatalf("CreateClient failed: %v", err)
	}
	if !existing[clientID] {
		t.Fatal("Client should exist after create")
	}

	// Delete
	if err := client.DeleteClient(clientID); err != nil {
		t.Fatalf("DeleteClient failed: %v", err)
	}
	if existing[clientID] {
		t.Error("Client should not exist after delete")
	}
}
