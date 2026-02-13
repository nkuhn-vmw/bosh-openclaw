package broker

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
)

type BindRequest struct {
	ServiceID  string                 `json:"service_id"`
	PlanID     string                 `json:"plan_id"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

type BindResponse struct {
	Credentials map[string]interface{} `json:"credentials"`
}

func (b *Broker) Bind(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	instanceID := vars["instance_id"]

	b.mu.RLock()
	instance, exists := b.instances[instanceID]
	b.mu.RUnlock()

	if !exists {
		http.Error(w, `{"error": "Instance not found"}`, http.StatusNotFound)
		return
	}

	if instance.State != "ready" {
		http.Error(w, `{"error": "Instance not ready"}`, http.StatusUnprocessableEntity)
		return
	}

	resp := BindResponse{
		Credentials: map[string]interface{}{
			"webchat_url":      fmt.Sprintf("https://%s.%s", instance.RouteHostname, instance.AppsDomain),
			"gateway_url":      fmt.Sprintf("wss://%s.agents.internal:18789", instance.DeploymentName),
			"gateway_token":    instance.GatewayToken,
			"api_endpoint":     fmt.Sprintf("https://%s.%s/api", instance.RouteHostname, instance.AppsDomain),
			"instance_id":      instance.ID,
			"owner":            instance.Owner,
			"plan":             instance.PlanName,
			"openclaw_version": instance.OpenClawVersion,
			"node_seed":        instance.NodeSeed,
			"sso_enabled":      instance.SSOEnabled,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (b *Broker) Unbind(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{})
}
