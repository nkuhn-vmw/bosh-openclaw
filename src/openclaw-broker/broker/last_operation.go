package broker

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
)

type LastOperationResponse struct {
	State       string `json:"state"`
	Description string `json:"description,omitempty"`
}

func (b *Broker) LastOperation(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	instanceID := vars["instance_id"]

	b.mu.RLock()
	instance, exists := b.instances[instanceID]
	b.mu.RUnlock()

	if !exists {
		http.Error(w, `{"error": "Instance not found"}`, http.StatusNotFound)
		return
	}

	var resp LastOperationResponse

	switch instance.State {
	case "provisioning":
		taskState, _ := b.director.TaskStatus(instance.BoshTaskID)
		switch taskState {
		case "done":
			b.mu.Lock()
			instance.State = "ready"
			b.mu.Unlock()
			resp = LastOperationResponse{State: "succeeded", Description: "Agent ready"}
		case "error", "cancelled":
			b.mu.Lock()
			instance.State = "failed"
			b.mu.Unlock()
			resp = LastOperationResponse{State: "failed", Description: "BOSH deployment failed"}
		default:
			resp = LastOperationResponse{State: "in progress", Description: "Deploying agent VM..."}
		}
	case "deprovisioning":
		taskState, _ := b.director.TaskStatus(instance.BoshTaskID)
		switch taskState {
		case "done":
			b.mu.Lock()
			delete(b.instances, instanceID)
			b.mu.Unlock()
			resp = LastOperationResponse{State: "succeeded", Description: "Agent deprovisioned"}
		case "error", "cancelled":
			resp = LastOperationResponse{State: "failed", Description: "Deprovision failed"}
		default:
			resp = LastOperationResponse{State: "in progress", Description: "Deprovisioning agent VM..."}
		}
	case "ready":
		resp = LastOperationResponse{State: "succeeded", Description: "Agent ready"}
	case "failed":
		resp = LastOperationResponse{State: "failed", Description: "Deployment failed"}
	default:
		resp = LastOperationResponse{State: "in progress", Description: "Processing..."}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
