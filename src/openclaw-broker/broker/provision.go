package broker

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/bosh"
	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/security"
)

type ProvisionRequest struct {
	ServiceID        string                 `json:"service_id"`
	PlanID           string                 `json:"plan_id"`
	OrganizationGUID string                 `json:"organization_guid"`
	SpaceGUID        string                 `json:"space_guid"`
	Parameters       map[string]interface{} `json:"parameters,omitempty"`
}

type ProvisionResponse struct {
	DashboardURL string `json:"dashboard_url,omitempty"`
	Operation    string `json:"operation,omitempty"`
}

func (b *Broker) Provision(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	instanceID := vars["instance_id"]

	var req ProvisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Bad request"}`, http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if already exists
	if _, exists := b.instances[instanceID]; exists {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "Instance already exists"})
		return
	}

	// Enforce quota limits
	if b.config.MaxInstances > 0 && b.countInstances() >= b.config.MaxInstances {
		log.Printf("Quota exceeded: %d/%d total instances", b.countInstances(), b.config.MaxInstances)
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"error":       "Quota exceeded",
			"description": fmt.Sprintf("Maximum total instances (%d) reached", b.config.MaxInstances),
		})
		return
	}
	if b.config.MaxInstancesPerOrg > 0 && b.countInstancesByOrg(req.OrganizationGUID) >= b.config.MaxInstancesPerOrg {
		log.Printf("Quota exceeded: org %s has %d/%d instances", req.OrganizationGUID, b.countInstancesByOrg(req.OrganizationGUID), b.config.MaxInstancesPerOrg)
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"error":       "Quota exceeded",
			"description": fmt.Sprintf("Maximum instances per org (%d) reached", b.config.MaxInstancesPerOrg),
		})
		return
	}

	// Enforce minimum OpenClaw version (CVE-2026-25253)
	openclawVersion := b.config.OpenClawVersion
	if v, ok := req.Parameters["openclaw_version"]; ok {
		openclawVersion = fmt.Sprintf("%v", v)
	}
	if b.config.MinOpenClawVersion != "" {
		if err := security.ValidateVersion(openclawVersion, b.config.MinOpenClawVersion); err != nil {
			log.Printf("Version gate rejected %s for %s: %v", openclawVersion, instanceID, err)
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{
				"error":       "Version below minimum safe version",
				"description": err.Error(),
			})
			return
		}
	}

	// Enforce Control UI disabled by default (CVE-2026-25253 mitigation)
	controlUIEnabled := b.config.ControlUIEnabled
	if cui, ok := req.Parameters["control_ui_enabled"]; ok {
		if v, ok := cui.(bool); ok {
			controlUIEnabled = v
		}
	}

	// Find plan
	plan := b.findPlan(req.PlanID)
	if plan == nil {
		http.Error(w, `{"error": "Unknown plan"}`, http.StatusBadRequest)
		return
	}

	// Generate credentials
	gatewayToken := security.GenerateGatewayToken()
	nodeSeed := security.GenerateNodeSeed()

	// Derive route hostname
	owner := "user"
	if o, ok := req.Parameters["owner"]; ok {
		owner = fmt.Sprintf("%v", o)
	}
	routeHostname := fmt.Sprintf("openclaw-%s", sanitizeHostname(owner))

	deploymentName := fmt.Sprintf("openclaw-agent-%s", instanceID)

	instance := &Instance{
		ID:               instanceID,
		PlanID:           req.PlanID,
		PlanName:         plan.Name,
		Owner:            owner,
		OrgGUID:          req.OrganizationGUID,
		SpaceGUID:        req.SpaceGUID,
		DeploymentName:   deploymentName,
		GatewayToken:     gatewayToken,
		NodeSeed:         nodeSeed,
		RouteHostname:    routeHostname,
		AppsDomain:       b.config.AppsDomain,
		VMType:           plan.VMType,
		DiskType:         plan.DiskType,
		State:            "provisioning",
		SSOEnabled:       b.config.SSOEnabled,
		ControlUIEnabled: controlUIEnabled,
		OpenClawVersion:  openclawVersion,
	}

	// Validate required infrastructure config
	if len(b.config.AZs) == 0 {
		log.Printf("No AZs configured for on-demand deployments")
		http.Error(w, `{"error": "Broker misconfiguration: no availability zones configured"}`, http.StatusInternalServerError)
		return
	}

	// Build manifest params and deploy via BOSH
	params := b.buildManifestParams(instance)
	manifest, err := bosh.RenderAgentManifest(params)
	if err != nil {
		log.Printf("Manifest render failed for %s: %v", instanceID, err)
		http.Error(w, `{"error": "Failed to render deployment manifest"}`, http.StatusInternalServerError)
		return
	}
	taskID, err := b.director.Deploy(manifest)
	if err != nil {
		log.Printf("BOSH deploy failed for %s: %v", instanceID, err)
		http.Error(w, `{"error": "Deployment failed"}`, http.StatusInternalServerError)
		return
	}

	instance.BoshTaskID = taskID
	b.instances[instanceID] = instance

	resp := ProvisionResponse{
		DashboardURL: fmt.Sprintf("https://%s", routeHostname),
		Operation:    fmt.Sprintf("provision-%s", instanceID),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(resp)
}

func (b *Broker) buildManifestParams(instance *Instance) bosh.ManifestParams {
	network := b.config.Network
	if network == "" {
		network = "default"
	}
	stemcellOS := b.config.StemcellOS
	if stemcellOS == "" {
		stemcellOS = "ubuntu-jammy"
	}
	stemcellVersion := b.config.StemcellVersion
	if stemcellVersion == "" {
		stemcellVersion = "latest"
	}
	azs := b.config.AZs
	sandboxMode := b.config.SandboxMode
	if sandboxMode == "" {
		sandboxMode = "strict"
	}
	cfDeploymentName := b.config.CFDeploymentName
	if cfDeploymentName == "" {
		cfDeploymentName = "cf"
	}
	openclawReleaseVersion := b.config.OpenClawReleaseVersion
	if openclawReleaseVersion == "" {
		openclawReleaseVersion = "latest"
	}
	bpmReleaseVersion := b.config.BPMReleaseVersion
	if bpmReleaseVersion == "" {
		bpmReleaseVersion = "latest"
	}
	routingReleaseVersion := b.config.RoutingReleaseVersion
	if routingReleaseVersion == "" {
		routingReleaseVersion = "latest"
	}

	return bosh.ManifestParams{
		DeploymentName:         instance.DeploymentName,
		ID:                     instance.ID,
		Owner:                  instance.Owner,
		PlanName:               instance.PlanName,
		GatewayToken:           instance.GatewayToken,
		NodeSeed:               instance.NodeSeed,
		RouteHostname:          instance.RouteHostname,
		VMType:                 instance.VMType,
		DiskType:               instance.DiskType,
		ControlUIEnabled:       instance.ControlUIEnabled,
		SSOEnabled:             instance.SSOEnabled,
		OpenClawVersion:        instance.OpenClawVersion,
		SandboxMode:            sandboxMode,
		Network:                network,
		AZs:                    azs,
		StemcellOS:             stemcellOS,
		StemcellVersion:        stemcellVersion,
		CFDeploymentName:       cfDeploymentName,
		OpenClawReleaseVersion: openclawReleaseVersion,
		BPMReleaseVersion:      bpmReleaseVersion,
		RoutingReleaseVersion:  routingReleaseVersion,
	}
}

func sanitizeHostname(s string) string {
	s = strings.ToLower(s)
	s = strings.Split(s, "@")[0]
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}
