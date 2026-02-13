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
            version: "{{ .OpenClawVersion }}"
            gateway:
              token: "{{ .GatewayToken }}"
              port: 18789
              websocket_origin_check: true
            webchat:
              enabled: true
              port: 8080
            control_ui:
              enabled: {{ .ControlUIEnabled }}
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

      - name: route_registrar
        release: routing
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
    azs: [{{ .AZ }}]
    persistent_disk_type: {{ .DiskType }}
    networks:
      - name: {{ .Network }}

stemcells:
  - alias: default
    os: {{ .StemcellOS }}
    version: "{{ .StemcellVersion }}"

releases:
  - name: openclaw
    version: latest
  - name: routing
    version: latest

update:
  canaries: 1
  max_in_flight: 1
  canary_watch_time: 30000-120000
  update_watch_time: 30000-120000
`

type ManifestParams struct {
	DeploymentName  string
	ID              string
	Owner           string
	PlanName        string
	GatewayToken    string
	NodeSeed        string
	RouteHostname   string
	VMType          string
	DiskType        string
	ControlUIEnabled bool
	OpenClawVersion string
	Network         string
	AZ              string
	StemcellOS      string
	StemcellVersion string
}

func RenderAgentManifest(params ManifestParams) []byte {
	tmpl, err := template.New("manifest").Parse(agentManifestTemplate)
	if err != nil {
		return nil
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return nil
	}
	return buf.Bytes()
}
