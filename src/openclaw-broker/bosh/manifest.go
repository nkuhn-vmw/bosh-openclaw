package bosh

import (
	"bytes"
	"text/template"
)

const agentManifestTemplate = `---
name: {{ .DeploymentName }}

instance_groups:
  - name: agent
    instances: 1
    jobs:
      - name: openclaw-agent
        release: openclaw
        properties:
          openclaw:
            version: "2026.2.10"
            gateway:
              token: "{{ .GatewayToken }}"
              port: 18789
            webchat:
              enabled: true
              port: 8080
            control_ui:
              enabled: false
              require_auth: true
            security:
              sandbox_mode: strict
            node:
              enabled: true
              seed: "{{ .NodeSeed }}"
            instance:
              id: "{{ .ID }}"
              owner: "{{ .Owner }}"
              plan: "{{ .PlanName }}"
            route:
              hostname: "{{ .RouteHostname }}"

      - name: route-registrar
        release: openclaw
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
    azs: [z1]
    persistent_disk_type: {{ .DiskType }}
    networks:
      - name: openclaw-agents

stemcells:
  - alias: default
    os: ubuntu-jammy
    version: latest

releases:
  - name: openclaw
    version: latest

update:
  canaries: 1
  max_in_flight: 1
  canary_watch_time: 30000-120000
  update_watch_time: 30000-120000
`

type ManifestParams struct {
	DeploymentName string
	ID             string
	Owner          string
	PlanName       string
	GatewayToken   string
	NodeSeed       string
	RouteHostname  string
	VMType         string
	DiskType       string
}

func (c *Client) RenderAgentManifest(deploymentName string, instance interface{}) []byte {
	// Type-assert the instance to extract fields
	type instanceLike interface {
		GetManifestParams() ManifestParams
	}

	// For simplicity, use a basic approach
	tmpl, err := template.New("manifest").Parse(agentManifestTemplate)
	if err != nil {
		return nil
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, instance); err != nil {
		return nil
	}
	return buf.Bytes()
}
