package broker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/bosh"
)

// writeJSON writes a JSON response with the given status code.
// All OSB API responses must be application/json.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type BrokerConfig struct {
	MinOpenClawVersion     string   `json:"min_openclaw_version"`
	ControlUIEnabled       bool     `json:"control_ui_enabled"`
	SandboxMode            string   `json:"sandbox_mode"`
	OpenClawVersion        string   `json:"openclaw_version"`
	Plans                  []Plan   `json:"plans"`
	AppsDomain             string   `json:"apps_domain"`
	Network                string   `json:"network"`
	AZs                    []string `json:"azs"`
	StemcellOS             string   `json:"stemcell_os"`
	StemcellVersion        string   `json:"stemcell_version"`
	CFDeploymentName       string   `json:"cf_deployment_name"`
	OpenClawReleaseVersion string   `json:"openclaw_release_version"`
	BPMReleaseVersion      string   `json:"bpm_release_version"`
	RoutingReleaseVersion  string   `json:"routing_release_version"`
	SSOEnabled             bool     `json:"sso_enabled"`
	SSOClientID            string   `json:"sso_client_id"`
	SSOClientSecret        string   `json:"sso_client_secret"`
	SSOCookieSecret        string   `json:"sso_cookie_secret"`
	SSOOIDCIssuerURL       string   `json:"sso_oidc_issuer_url"`
	MaxInstances           int      `json:"max_instances"`
	MaxInstancesPerOrg     int      `json:"max_instances_per_org"`
}

type Broker struct {
	config    BrokerConfig
	director  *bosh.Client
	mu        sync.RWMutex
	instances map[string]*Instance
}

type Instance struct {
	ID             string `json:"id"`
	PlanID         string `json:"plan_id"`
	PlanName       string `json:"plan_name"`
	Owner          string `json:"owner"`
	OrgGUID        string `json:"org_guid"`
	SpaceGUID      string `json:"space_guid"`
	DeploymentName string `json:"deployment_name"`
	GatewayToken   string `json:"gateway_token"`
	NodeSeed       string `json:"node_seed"`
	RouteHostname  string `json:"route_hostname"`
	AppsDomain     string `json:"apps_domain"`
	VMType         string `json:"vm_type"`
	DiskType       string `json:"disk_type"`
	State            string `json:"state"` // provisioning, ready, deprovisioning, failed
	BoshTaskID       int    `json:"bosh_task_id"`
	SSOEnabled       bool   `json:"sso_enabled"`
	ControlUIEnabled bool   `json:"control_ui_enabled"`
	OpenClawVersion  string `json:"openclaw_version"`
}

type Plan struct {
	Name            string                 `json:"name"`
	ID              string                 `json:"id"`
	Description     string                 `json:"description"`
	PlanDescription string                 `json:"plan_description"` // OpsMan service_plan_forms field
	VMType          string                 `json:"vm_type"`
	DiskType        string                 `json:"disk_type"`
	Memory          int                    `json:"memory"`
	Features        map[string]bool        `json:"features,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

func New(config BrokerConfig, director *bosh.Client) *Broker {
	normalizePlans(config.Plans)
	return &Broker{
		config:    config,
		director:  director,
		instances: make(map[string]*Instance),
	}
}

// normalizePlans fills in missing ID and Description fields for plans coming
// from OpsMan service_plan_forms, which uses "plan_description" instead of
// "description" and doesn't emit an "id" field.
func normalizePlans(plans []Plan) {
	for i := range plans {
		if plans[i].ID == "" && plans[i].Name != "" {
			plans[i].ID = fmt.Sprintf("openclaw-%s-plan", plans[i].Name)
		}
		if plans[i].Description == "" && plans[i].PlanDescription != "" {
			plans[i].Description = plans[i].PlanDescription
		}
		if plans[i].Description == "" && plans[i].Name != "" {
			plans[i].Description = fmt.Sprintf("OpenClaw %s plan", plans[i].Name)
		}
	}
}

// countInstances returns the total number of active (non-deprovisioning) instances.
// Must be called with b.mu held.
func (b *Broker) countInstances() int {
	count := 0
	for _, inst := range b.instances {
		if inst.State != "deprovisioning" {
			count++
		}
	}
	return count
}

// countInstancesByOrg returns the number of active instances for a given org.
// Must be called with b.mu held.
func (b *Broker) countInstancesByOrg(orgGUID string) int {
	count := 0
	for _, inst := range b.instances {
		if inst.OrgGUID == orgGUID && inst.State != "deprovisioning" {
			count++
		}
	}
	return count
}

// findPlan searches config plans by ID, falling back to hardcoded defaults.
func (b *Broker) findPlan(planID string) *Plan {
	plans := b.config.Plans
	if len(plans) == 0 {
		plans = defaultPlans()
	}
	for i := range plans {
		if plans[i].ID == planID {
			return &plans[i]
		}
	}
	return nil
}

func defaultPlans() []Plan {
	return []Plan{
		{
			ID:          "openclaw-developer-plan",
			Name:        "developer",
			Description: "Dedicated OpenClaw agent for individual developers",
			VMType:      "small",
			DiskType:    "10GB",
			Memory:      2048,
		},
		{
			ID:          "openclaw-developer-plus-plan",
			Name:        "developer-plus",
			Description: "Enhanced agent with browser automation",
			VMType:      "medium",
			DiskType:    "20GB",
			Memory:      4096,
		},
		{
			ID:          "openclaw-team-plan",
			Name:        "team",
			Description: "Shared agent for teams",
			VMType:      "large",
			DiskType:    "50GB",
			Memory:      8192,
		},
	}
}
