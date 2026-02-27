package broker

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/bosh"
	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/uaa"
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
	SSOEnabled              bool   `json:"sso_enabled"`
	SSOOIDCIssuerURL        string `json:"sso_oidc_issuer_url"`
	SSOAllowedEmailDomains  string `json:"sso_allowed_email_domains"`
	SSOSessionTimeoutHours  int    `json:"sso_session_timeout_hours"`
	CFUaaURL                string `json:"cf_uaa_url"`
	CFUaaAdminClientID      string `json:"cf_uaa_admin_client_id"`
	CFUaaAdminClientSecret  string `json:"cf_uaa_admin_client_secret"`
	MaxInstances           int      `json:"max_instances"`
	MaxInstancesPerOrg     int      `json:"max_instances_per_org"`
	LLMProvider            string   `json:"llm_provider"`
	LLMEndpoint            string   `json:"llm_endpoint"`
	LLMAPIKey              string   `json:"llm_api_key"`
	LLMModel               string   `json:"llm_model"`
	LLMPreferredModel      string   `json:"llm_preferred_model"`
	LLMAPIEndpoint         string   `json:"llm_api_endpoint"`
	GenAIOfferingName      string   `json:"genai_offering_name"`
	GenAIPlanName          string   `json:"genai_plan_name"`
	BlockedCommands        string   `json:"blocked_commands"`
	NATSTLSEnabled         bool     `json:"nats_tls_enabled"`
	NATSTLSClientCert      string   `json:"nats_tls_client_cert"`
	NATSTLSClientKey       string   `json:"nats_tls_client_key"`
	NATSTLSCACert          string   `json:"nats_tls_ca_cert"`
	StateDir               string   `json:"state_dir"`
}

type upgradeTracker struct {
	mu    sync.Mutex
	tasks map[string]int // instanceID -> BOSH task ID
}

type Broker struct {
	config    BrokerConfig
	director  *bosh.Client
	uaaClient *uaa.Client
	mu        sync.RWMutex
	instances map[string]*Instance
	upgrades  upgradeTracker
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
	SSOClientID      string `json:"sso_client_id,omitempty"`
	SSOClientSecret  string `json:"sso_client_secret,omitempty"`
	SSOCookieSecret  string `json:"sso_cookie_secret,omitempty"`
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
	AZs             []string               `json:"azs,omitempty"`
	Features        map[string]bool        `json:"features,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

func New(config BrokerConfig, director *bosh.Client) *Broker {
	normalizePlans(config.Plans)
	b := &Broker{
		config:    config,
		director:  director,
		instances: make(map[string]*Instance),
	}
	// Create UAA client for dynamic OAuth2 client management when SSO is enabled
	if config.SSOEnabled && config.CFUaaURL != "" && config.CFUaaAdminClientSecret != "" {
		b.uaaClient = uaa.NewClient(config.CFUaaURL, config.CFUaaAdminClientID, config.CFUaaAdminClientSecret, true)
	}
	b.loadState()
	return b
}

// saveState writes the instance map to disk for persistence across broker restarts.
// Acquires its own read lock; callers must NOT hold the lock.
func (b *Broker) saveState() {
	if b.config.StateDir == "" {
		return
	}
	b.mu.RLock()
	data, err := json.MarshalIndent(b.instances, "", "  ")
	b.mu.RUnlock()
	if err != nil {
		log.Printf("Failed to marshal state: %v", err)
		return
	}
	path := filepath.Join(b.config.StateDir, "instances.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Printf("Failed to write state file: %v", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("Failed to rename state file: %v", err)
	}
}

// loadState reads instance state from disk on startup.
func (b *Broker) loadState() {
	if b.config.StateDir == "" {
		return
	}
	path := filepath.Join(b.config.StateDir, "instances.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		log.Printf("Failed to read state file: %v", err)
		return
	}
	var instances map[string]*Instance
	if err := json.Unmarshal(data, &instances); err != nil {
		log.Printf("Failed to unmarshal state file: %v", err)
		return
	}
	b.instances = instances
	log.Printf("Loaded %d instances from state file", len(instances))
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
