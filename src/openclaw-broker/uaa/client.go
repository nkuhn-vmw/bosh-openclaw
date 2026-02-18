package uaa

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client provides UAA OAuth2 client management.
type Client struct {
	uaaURL     string
	adminID    string
	adminSecret string
	httpClient *http.Client
}

// NewClient creates a UAA client. uaaURL is the UAA base URL (e.g., https://uaa.sys.example.com).
// adminID and adminSecret are the UAA admin client credentials used to manage OAuth2 clients.
func NewClient(uaaURL, adminID, adminSecret string, skipSSLValidation bool) *Client {
	transport := &http.Transport{}
	if skipSSLValidation {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		uaaURL:      strings.TrimRight(uaaURL, "/"),
		adminID:     adminID,
		adminSecret: adminSecret,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

// tokenResponse is the OAuth2 token endpoint response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// getAdminToken obtains an access token via client_credentials grant.
func (c *Client) getAdminToken() (string, error) {
	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.adminID},
		"client_secret": {c.adminSecret},
	}

	req, err := http.NewRequest("POST", c.uaaURL+"/oauth/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("requesting admin token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}
	return tok.AccessToken, nil
}

// OAuthClient represents a UAA OAuth2 client registration.
type OAuthClient struct {
	ClientID             string   `json:"client_id"`
	ClientSecret         string   `json:"client_secret,omitempty"`
	AuthorizedGrantTypes []string `json:"authorized_grant_types"`
	RedirectURI          []string `json:"redirect_uri"`
	Scope                []string `json:"scope"`
	Authorities          []string `json:"authorities"`
	Name                 string   `json:"name,omitempty"`
}

// CreateClient registers a new OAuth2 client in UAA.
func (c *Client) CreateClient(client OAuthClient) error {
	token, err := c.getAdminToken()
	if err != nil {
		return fmt.Errorf("getting admin token: %w", err)
	}

	payload, err := json.Marshal(client)
	if err != nil {
		return fmt.Errorf("marshalling client: %w", err)
	}

	req, err := http.NewRequest("POST", c.uaaURL+"/oauth/clients", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("building create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("creating OAuth client: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusConflict {
		// Client already exists — idempotent
		return nil
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create client failed (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// DeleteClient removes an OAuth2 client from UAA.
func (c *Client) DeleteClient(clientID string) error {
	token, err := c.getAdminToken()
	if err != nil {
		return fmt.Errorf("getting admin token: %w", err)
	}

	req, err := http.NewRequest("DELETE", c.uaaURL+"/oauth/clients/"+url.PathEscape(clientID), nil)
	if err != nil {
		return fmt.Errorf("building delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deleting OAuth client: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Already deleted — idempotent
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete client failed (status %d): %s", resp.StatusCode, string(body))
	}
	return nil
}

// GenerateClientSecret creates a cryptographically random 32-byte hex secret.
func GenerateClientSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// GenerateCookieSecret creates a cryptographically random 32-byte hex secret
// suitable for oauth2-proxy cookie encryption.
func GenerateCookieSecret() string {
	return GenerateClientSecret()
}

// ClientIDForInstance returns the UAA client ID for an on-demand instance.
func ClientIDForInstance(instanceID string) string {
	return "openclaw-" + instanceID
}
