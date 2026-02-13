package bosh

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type uaaToken struct {
	accessToken string
	expiresAt   time.Time
}

type Client struct {
	directorURL  string
	clientID     string
	clientSecret string
	uaaURL       string
	httpClient   *http.Client
	token        *uaaToken
	tokenMu      sync.Mutex
}

func NewClient(directorURL, clientID, clientSecret, caCert, uaaURL string) *Client {
	tlsConfig := &tls.Config{}
	if caCert != "" {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM([]byte(caCert))
		tlsConfig.RootCAs = pool
	}

	return &Client{
		directorURL:  strings.TrimRight(directorURL, "/"),
		clientID:     clientID,
		clientSecret: clientSecret,
		uaaURL:       strings.TrimRight(uaaURL, "/"),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	}
}

func (c *Client) getToken() (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.token != nil && time.Now().Before(c.token.expiresAt) {
		return c.token.accessToken, nil
	}

	data := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
	}

	resp, err := c.httpClient.PostForm(c.uaaURL+"/oauth/token", data)
	if err != nil {
		return "", fmt.Errorf("UAA token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("UAA token request returned %d: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode UAA token response: %w", err)
	}

	// Cache token with 60s safety margin before expiry
	c.token = &uaaToken{
		accessToken: tokenResp.AccessToken,
		expiresAt:   time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second),
	}

	return c.token.accessToken, nil
}

// setAuth sets authorization on the request. Uses UAA bearer token if uaaURL is configured,
// otherwise falls back to basic auth (useful for tests and non-UAA environments).
func (c *Client) setAuth(req *http.Request) error {
	if c.uaaURL != "" {
		token, err := c.getToken()
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.SetBasicAuth(c.clientID, c.clientSecret)
	}
	return nil
}

func (c *Client) Deploy(manifest []byte) (int, error) {
	req, err := http.NewRequest("POST", c.directorURL+"/deployments", strings.NewReader(string(manifest)))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "text/yaml")
	if err := c.setAuth(req); err != nil {
		return 0, fmt.Errorf("failed to authenticate: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("deploy request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("deploy failed with status %d: %s", resp.StatusCode, body)
	}

	location := resp.Header.Get("Location")
	var taskID int
	if n, _ := fmt.Sscanf(location, "/tasks/%d", &taskID); n != 1 {
		return 0, fmt.Errorf("failed to parse task ID from Location header: %q", location)
	}
	return taskID, nil
}

func (c *Client) DeleteDeployment(name string) (int, error) {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/deployments/%s", c.directorURL, name), nil)
	if err != nil {
		return 0, err
	}
	if err := c.setAuth(req); err != nil {
		return 0, fmt.Errorf("failed to authenticate: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("delete request failed: %w", err)
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	var taskID int
	if n, _ := fmt.Sscanf(location, "/tasks/%d", &taskID); n != 1 {
		return 0, fmt.Errorf("failed to parse task ID from Location header: %q", location)
	}
	return taskID, nil
}

func (c *Client) TaskStatus(taskID int) (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/tasks/%d", c.directorURL, taskID), nil)
	if err != nil {
		return "", err
	}
	if err := c.setAuth(req); err != nil {
		return "", fmt.Errorf("failed to authenticate: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("task status request failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.State, nil
}
