package broker

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
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

	// Find plan
	planName := planNameFromID(req.PlanID)
	if planName == "" {
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
		ID:             instanceID,
		PlanID:         req.PlanID,
		PlanName:       planName,
		Owner:          owner,
		OrgGUID:        req.OrganizationGUID,
		SpaceGUID:      req.SpaceGUID,
		DeploymentName: deploymentName,
		GatewayToken:   gatewayToken,
		NodeSeed:       nodeSeed,
		RouteHostname:  routeHostname,
		State:          "provisioning",
	}

	// Deploy via BOSH
	manifest := b.director.RenderAgentManifest(instance.DeploymentName, instance)
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

func planNameFromID(planID string) string {
	switch planID {
	case "openclaw-developer-plan":
		return "developer"
	case "openclaw-developer-plus-plan":
		return "developer-plus"
	case "openclaw-team-plan":
		return "team"
	default:
		return ""
	}
}

func sanitizeHostname(s string) string {
	s = strings.ToLower(s)
	s = strings.Split(s, "@")[0]
	s = strings.ReplaceAll(s, ".", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}
