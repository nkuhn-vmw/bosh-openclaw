package broker

import (
	"sync"

	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/bosh"
)

type BrokerConfig struct {
	MinOpenClawVersion string `json:"min_openclaw_version"`
	ControlUIEnabled   bool   `json:"control_ui_enabled"`
	SandboxMode        string `json:"sandbox_mode"`
	OpenClawVersion    string `json:"openclaw_version"`
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
	State            string `json:"state"` // provisioning, ready, deprovisioning, failed
	BoshTaskID       int    `json:"bosh_task_id"`
	SSOEnabled       bool   `json:"sso_enabled"`
	ControlUIEnabled bool   `json:"control_ui_enabled"`
	OpenClawVersion  string `json:"openclaw_version"`
}

type Plan struct {
	Name        string                 `json:"name"`
	ID          string                 `json:"id"`
	Description string                 `json:"description"`
	VMType      string                 `json:"vm_type"`
	DiskType    string                 `json:"disk_type"`
	Memory      int                    `json:"memory"`
	Features    map[string]bool        `json:"features,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

func New(config BrokerConfig, director *bosh.Client) *Broker {
	return &Broker{
		config:    config,
		director:  director,
		instances: make(map[string]*Instance),
	}
}
