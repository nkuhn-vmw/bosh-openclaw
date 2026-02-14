package broker

import (
	"encoding/json"
	"log"
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

	// Read instance state and task ID under lock, then release before
	// making BOSH HTTP calls to avoid blocking other operations.
	b.mu.RLock()
	instance, exists := b.instances[instanceID]
	if !exists {
		b.mu.RUnlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Instance not found"})
		return
	}
	state := instance.State
	taskID := instance.BoshTaskID
	b.mu.RUnlock()

	var resp LastOperationResponse

	switch state {
	case "provisioning":
		if taskID == 0 {
			resp = LastOperationResponse{State: "in progress", Description: "Waiting for deployment task..."}
			break
		}
		taskState, err := b.director.TaskStatus(taskID)
		if err != nil {
			log.Printf("TaskStatus error for %s (task %d): %v", instanceID, taskID, err)
		}
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
		if taskID == 0 {
			resp = LastOperationResponse{State: "in progress", Description: "Waiting for delete task..."}
			break
		}
		taskState, err := b.director.TaskStatus(taskID)
		if err != nil {
			log.Printf("TaskStatus error for %s (task %d): %v", instanceID, taskID, err)
		}
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
