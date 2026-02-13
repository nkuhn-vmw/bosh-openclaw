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

	// Use a write lock for the entire operation to prevent races between
	// reading the instance state and mutating it based on BOSH task status.
	b.mu.Lock()
	instance, exists := b.instances[instanceID]
	if !exists {
		b.mu.Unlock()
		http.Error(w, `{"error": "Instance not found"}`, http.StatusNotFound)
		return
	}

	var resp LastOperationResponse

	switch instance.State {
	case "provisioning":
		taskState, _ := b.director.TaskStatus(instance.BoshTaskID)
		switch taskState {
		case "done":
			instance.State = "ready"
			resp = LastOperationResponse{State: "succeeded", Description: "Agent ready"}
		case "error", "cancelled":
			instance.State = "failed"
			resp = LastOperationResponse{State: "failed", Description: "BOSH deployment failed"}
		default:
			resp = LastOperationResponse{State: "in progress", Description: "Deploying agent VM..."}
		}
	case "deprovisioning":
		taskState, _ := b.director.TaskStatus(instance.BoshTaskID)
		switch taskState {
		case "done":
			delete(b.instances, instanceID)
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
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
