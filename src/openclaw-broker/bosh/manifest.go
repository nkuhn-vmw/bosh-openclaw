package bosh

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
)

const agentManifestTemplate = `---
name: {{ .DeploymentName }}

instance_groups:
  - name: agent
    instances: 1
    jobs:
      - name: bpm
        release: bpm

      - name: openclaw-agent
        release: openclaw
        properties:
          openclaw:
            version: "{{ .OpenClawVersion }}"
            gateway:
              token: "{{ .GatewayToken }}"
              port: 18789
            webchat:
              enabled: true
              port: {{ if .SSOEnabled }}8081{{ else }}8080{{ end }}
            control_ui:
              enabled: {{ .ControlUIEnabled }}
              require_auth: true
            security:
              sandbox_mode: {{ .SandboxMode }}
{{- if .BlockedCommands }}
              blocked_commands:
{{- range .BlockedCommands }}
                - "{{ . }}"
{{- end }}
{{- end }}
{{- if .LLMProvider }}
            llm:
              provider: "{{ .LLMProvider }}"
{{- if or .LLMEndpoint .LLMAPIEndpoint }}
              genai:
{{- if .LLMEndpoint }}
                endpoint: "{{ .LLMEndpoint }}"
{{- else if .LLMAPIEndpoint }}
                endpoint: "{{ .LLMAPIEndpoint }}"
{{- end }}
{{- if .LLMAPIKey }}
                api_key: "{{ .LLMAPIKey }}"
{{- end }}
{{- if .LLMModel }}
                model: "{{ .LLMModel }}"
{{- end }}
{{- end }}
{{- if not (or .LLMEndpoint .LLMAPIEndpoint) }}
{{- if .LLMAPIKey }}
              genai:
                api_key: "{{ .LLMAPIKey }}"
{{- if .LLMModel }}
                model: "{{ .LLMModel }}"
{{- end }}
{{- end }}
{{- end }}
{{- end }}
            browser:
              enabled: {{ .BrowserEnabled }}
            node:
              enabled: true
              seed: "{{ .NodeSeed }}"
            instance:
              id: "{{ .ID }}"
              owner: "{{ .Owner }}"
              plan: "{{ .PlanName }}"
            route:
              hostname: "{{ .RouteHostname }}"
              domain: "{{ .AppsDomain }}"
{{- if .SSOEnabled }}
            sso:
              enabled: true
{{- end }}
{{ if .SSOEnabled }}
      - name: openclaw-sso-proxy
        release: openclaw
        properties:
          openclaw:
            sso_proxy:
              listen_port: 8080
              upstream_port: 8081
              client_id: "{{ .SSOClientID }}"
              client_secret: "{{ .SSOClientSecret }}"
              cookie_secret: "{{ .SSOCookieSecret }}"
{{- if .SSOOIDCIssuerURL }}
              oidc_issuer_url: "{{ .SSOOIDCIssuerURL }}"
{{- end }}
{{ end }}
      - name: route_registrar
        release: routing
        consumes:
          nats-tls:
            from: nats-tls
            deployment: {{ .CFDeploymentName }}
        properties:
          nats:
            machines:
              - q-s0.nats.default.{{ .CFDeploymentName }}.bosh
            port: 4222
            tls:
              enabled: true
{{- if .NATSTLSClientCert }}
              client_cert: |
{{ indent 16 .NATSTLSClientCert }}
{{- end }}
{{- if .NATSTLSClientKey }}
              private_key: |
{{ indent 16 .NATSTLSClientKey }}
{{- end }}
{{- if .NATSTLSCACert }}
              ca_certs: |
{{ indent 16 .NATSTLSCACert }}
{{- end }}
          route_registrar:
            routes:
              - name: "openclaw-{{ .ID }}"
                registration_interval: 20s
                port: 8080
                uris:
                  - "{{ .RouteHostname }}.{{ .AppsDomain }}"

    vm_type: {{ .VMType }}
    stemcell: default
    azs: [{{ .AZsYAML }}]
    persistent_disk_type: {{ .DiskType }}
    networks:
      - name: {{ .Network }}

stemcells:
  - alias: default
    os: {{ .StemcellOS }}
    version: "{{ .StemcellVersion }}"

releases:
  - name: openclaw
    version: "{{ .OpenClawReleaseVersion }}"
  - name: bpm
    version: "{{ .BPMReleaseVersion }}"
  - name: routing
    version: "{{ .RoutingReleaseVersion }}"

update:
  canaries: 1
  max_in_flight: 1
  canary_watch_time: 30000-120000
  update_watch_time: 30000-120000
`

type ManifestParams struct {
	DeploymentName        string
	ID                    string
	Owner                 string
	PlanName              string
	GatewayToken          string
	NodeSeed              string
	RouteHostname         string
	VMType                string
	DiskType              string
	ControlUIEnabled      bool
	SSOEnabled            bool
	OpenClawVersion       string
	SandboxMode           string
	Network               string
	AZs                   []string
	StemcellOS            string
	StemcellVersion       string
	CFDeploymentName      string
	OpenClawReleaseVersion string
	BPMReleaseVersion      string
	RoutingReleaseVersion  string
	AppsDomain             string
	SSOClientID            string
	SSOClientSecret        string
	SSOCookieSecret        string
	SSOOIDCIssuerURL       string
	LLMProvider            string
	LLMEndpoint            string
	LLMAPIKey              string
	LLMModel               string
	LLMAPIEndpoint         string
	BrowserEnabled         bool
	BlockedCommands        []string
	NATSTLSClientCert      string
	NATSTLSClientKey       string
	NATSTLSCACert          string
}

// AZsYAML returns the AZs formatted for inline YAML: "az1, az2"
func (p ManifestParams) AZsYAML() string {
	return strings.Join(p.AZs, ", ")
}

// yamlSafeControlChars strips control characters and escapes double quotes and
// backslashes in strings that will be placed inside YAML double-quoted values.
// This prevents YAML injection via user-supplied fields like Owner.
var unsafeYAMLChars = regexp.MustCompile(`[^\x20-\x7E]`)

func sanitizeForYAML(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = unsafeYAMLChars.ReplaceAllString(s, "")
	return s
}

// indent returns s with each line prefixed by the given number of spaces.
// Used in the manifest template to indent PEM certificates inside YAML block scalars.
func indentPEM(spaces int, s string) string {
	if s == "" {
		return ""
	}
	pad := strings.Repeat(" ", spaces)
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func RenderAgentManifest(params ManifestParams) ([]byte, error) {
	// Sanitize strings that go into YAML double-quoted values.
	// PEM certs (NATSTLSClientCert/Key) are NOT sanitized because they go
	// inside YAML block scalars and contain multi-line base64 content.
	params.Owner = sanitizeForYAML(params.Owner)
	params.RouteHostname = sanitizeForYAML(params.RouteHostname)
	params.SSOClientID = sanitizeForYAML(params.SSOClientID)
	params.SSOClientSecret = sanitizeForYAML(params.SSOClientSecret)
	params.SSOCookieSecret = sanitizeForYAML(params.SSOCookieSecret)
	params.SSOOIDCIssuerURL = sanitizeForYAML(params.SSOOIDCIssuerURL)
	params.LLMProvider = sanitizeForYAML(params.LLMProvider)
	params.LLMEndpoint = sanitizeForYAML(params.LLMEndpoint)
	params.LLMAPIKey = sanitizeForYAML(params.LLMAPIKey)
	params.LLMModel = sanitizeForYAML(params.LLMModel)
	params.LLMAPIEndpoint = sanitizeForYAML(params.LLMAPIEndpoint)
	for i := range params.BlockedCommands {
		params.BlockedCommands[i] = sanitizeForYAML(params.BlockedCommands[i])
	}

	tmpl, err := template.New("manifest").Funcs(template.FuncMap{
		"indent": indentPEM,
	}).Parse(agentManifestTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return nil, fmt.Errorf("failed to render manifest: %w", err)
	}
	return buf.Bytes(), nil
}
