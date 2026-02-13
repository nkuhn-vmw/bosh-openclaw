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
              websocket_origin_check: true
            webchat:
              enabled: true
              port: {{ if .SSOEnabled }}8081{{ else }}8080{{ end }}
            control_ui:
              enabled: {{ .ControlUIEnabled }}
              require_auth: true
            security:
              sandbox_mode: {{ .SandboxMode }}
            node:
              enabled: true
              seed: "{{ .NodeSeed }}"
            instance:
              id: "{{ .ID }}"
              owner: "{{ .Owner }}"
              plan: "{{ .PlanName }}"
            route:
              hostname: "{{ .RouteHostname }}"
{{ if .SSOEnabled }}
      - name: openclaw-sso-proxy
        release: openclaw
        properties:
          openclaw:
            sso_proxy:
              listen_port: 8080
              upstream_port: 8081
{{ end }}
      - name: route_registrar
        release: routing
        consumes:
          nats-tls:
            from: nats-tls
            deployment: {{ .CFDeploymentName }}
        properties:
          route_registrar:
            routes:
              - name: "openclaw-{{ .ID }}"
                registration_interval: 20s
                port: 8080
                uris:
                  - "{{ .RouteHostname }}"

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
	BPMReleaseVersion     string
	RoutingReleaseVersion string
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

func RenderAgentManifest(params ManifestParams) ([]byte, error) {
	// Sanitize user-supplied strings that go into YAML quoted values
	params.Owner = sanitizeForYAML(params.Owner)
	params.RouteHostname = sanitizeForYAML(params.RouteHostname)

	tmpl, err := template.New("manifest").Parse(agentManifestTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return nil, fmt.Errorf("failed to render manifest: %w", err)
	}
	return buf.Bytes(), nil
}
