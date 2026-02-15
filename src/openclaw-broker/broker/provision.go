package broker

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"
	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/bosh"
	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/security"
)

// validInstanceID matches OSB instance IDs: alphanumeric, hyphens, underscores, max 64 chars.
var validInstanceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

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

	// Validate instance ID to prevent YAML injection via crafted IDs
	if !validInstanceID.MatchString(instanceID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid instance_id format"})
		return
	}

	// OSB API: async operations require accepts_incomplete=true
	if r.URL.Query().Get("accepts_incomplete") != "true" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]string{
			"error":       "AsyncRequired",
			"description": "This service plan requires client support for asynchronous service operations.",
		})
		return
	}

	var req ProvisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Bad request"})
		return
	}

	b.mu.Lock()

	// Check if already exists
	if _, exists := b.instances[instanceID]; exists {
		b.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Instance already exists"})
		return
	}

	// Enforce quota limits
	if b.config.MaxInstances > 0 && b.countInstances() >= b.config.MaxInstances {
		log.Printf("Quota exceeded: %d/%d total instances", b.countInstances(), b.config.MaxInstances)
		b.mu.Unlock()
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
			"error":       "Quota exceeded",
			"description": fmt.Sprintf("Maximum total instances (%d) reached", b.config.MaxInstances),
		})
		return
	}
	if b.config.MaxInstancesPerOrg > 0 && b.countInstancesByOrg(req.OrganizationGUID) >= b.config.MaxInstancesPerOrg {
		log.Printf("Quota exceeded: org %s has %d/%d instances", req.OrganizationGUID, b.countInstancesByOrg(req.OrganizationGUID), b.config.MaxInstancesPerOrg)
		b.mu.Unlock()
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
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
			b.mu.Unlock()
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
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
		b.mu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Unknown plan"})
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
	sanitizedOwner := sanitizeHostname(owner)
	if sanitizedOwner == "" {
		sanitizedOwner = "agent"
	}
	routeHostname := fmt.Sprintf("openclaw-%s", sanitizedOwner)

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
		b.mu.Unlock()
		log.Printf("No AZs configured for on-demand deployments")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Broker misconfiguration: no availability zones configured"})
		return
	}
	if b.config.AppsDomain == "" {
		b.mu.Unlock()
		log.Printf("No apps domain configured")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Broker misconfiguration: no apps domain configured"})
		return
	}

	// Reserve the instance slot before releasing the lock for the BOSH call.
	// This prevents duplicate provisions for the same instance ID.
	b.instances[instanceID] = instance
	b.mu.Unlock()

	// Build manifest params and deploy via BOSH (outside lock to avoid blocking)
	params := b.buildManifestParams(instance)
	manifest, err := bosh.RenderAgentManifest(params)
	if err != nil {
		log.Printf("Manifest render failed for %s: %v", instanceID, err)
		b.mu.Lock()
		delete(b.instances, instanceID)
		b.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to render deployment manifest"})
		return
	}
	taskID, err := b.director.Deploy(manifest)
	if err != nil {
		log.Printf("BOSH deploy failed for %s: %v", instanceID, err)
		b.mu.Lock()
		delete(b.instances, instanceID)
		b.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Deployment failed"})
		return
	}

	b.mu.Lock()
	instance.BoshTaskID = taskID
	b.mu.Unlock()

	resp := ProvisionResponse{
		DashboardURL: fmt.Sprintf("https://%s.%s", routeHostname, b.config.AppsDomain),
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

	// Determine browser automation from plan features
	browserEnabled := false
	plan := b.findPlan(instance.PlanID)
	if plan != nil && plan.Features["browser"] {
		browserEnabled = true
	}

	// Parse blocked commands: tile may send newline-separated or comma-separated
	var blockedCmds []string
	if b.config.BlockedCommands != "" {
		// Replace newlines with commas so we can split uniformly
		normalized := strings.ReplaceAll(b.config.BlockedCommands, "\n", ",")
		normalized = strings.ReplaceAll(normalized, "\r", "")
		for _, cmd := range strings.Split(normalized, ",") {
			cmd = strings.TrimSpace(cmd)
			if cmd != "" {
				blockedCmds = append(blockedCmds, cmd)
			}
		}
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
		AppsDomain:             instance.AppsDomain,
		SSOClientID:            b.config.SSOClientID,
		SSOClientSecret:        b.config.SSOClientSecret,
		SSOCookieSecret:        b.config.SSOCookieSecret,
		SSOOIDCIssuerURL:       b.config.SSOOIDCIssuerURL,
		LLMProvider:            b.config.LLMProvider,
		LLMEndpoint:            b.config.LLMEndpoint,
		LLMAPIKey:              b.config.LLMAPIKey,
		LLMModel:               b.config.LLMModel,
		LLMAPIEndpoint:         b.config.LLMAPIEndpoint,
		GenAIOfferingName:      b.config.GenAIOfferingName,
		GenAIPlanName:          b.config.GenAIPlanName,
		BrowserEnabled:         browserEnabled,
		BlockedCommands:        blockedCmds,
	}
}

// invalidDNSChars matches any character not valid in a DNS label.
var invalidDNSChars = regexp.MustCompile(`[^a-z0-9-]`)

func sanitizeHostname(s string) string {
	s = strings.ToLower(s)
	s = strings.Split(s, "@")[0]
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = invalidDNSChars.ReplaceAllString(s, "")
	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")
	return s
}
