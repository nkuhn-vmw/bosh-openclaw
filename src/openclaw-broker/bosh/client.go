package bosh

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	directorURL  string
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

func NewClient(directorURL, clientID, clientSecret, caCert string) *Client {
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
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	}
}

func (c *Client) Deploy(manifest []byte) (int, error) {
	req, err := http.NewRequest("POST", c.directorURL+"/deployments", strings.NewReader(string(manifest)))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "text/yaml")
	req.SetBasicAuth(c.clientID, c.clientSecret)

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
	fmt.Sscanf(location, "/tasks/%d", &taskID)
	return taskID, nil
}

func (c *Client) DeleteDeployment(name string) (int, error) {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/deployments/%s", c.directorURL, name), nil)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("delete request failed: %w", err)
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	var taskID int
	fmt.Sscanf(location, "/tasks/%d", &taskID)
	return taskID, nil
}

func (c *Client) TaskStatus(taskID int) (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/tasks/%d", c.directorURL, taskID), nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)

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
