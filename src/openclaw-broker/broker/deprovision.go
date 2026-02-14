package broker

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

type DeprovisionResponse struct {
	Operation string `json:"operation,omitempty"`
}

func (b *Broker) Deprovision(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	instanceID := vars["instance_id"]

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

	b.mu.Lock()

	instance, exists := b.instances[instanceID]
	if !exists {
		b.mu.Unlock()
		writeJSON(w, http.StatusGone, map[string]string{})
		return
	}

	// If already deprovisioning, return the existing operation (idempotent)
	if instance.State == "deprovisioning" {
		b.mu.Unlock()
		writeJSON(w, http.StatusAccepted, DeprovisionResponse{
			Operation: fmt.Sprintf("deprovision-%s", instanceID),
		})
		return
	}

	// Mark as deprovisioning and capture deployment name before releasing lock
	previousState := instance.State
	instance.State = "deprovisioning"
	deploymentName := instance.DeploymentName
	b.mu.Unlock()

	taskID, err := b.director.DeleteDeployment(deploymentName)
	if err != nil {
		log.Printf("BOSH delete failed for %s: %v", instanceID, err)
		// Restore previous state on failure
		b.mu.Lock()
		instance.State = previousState
		b.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Deprovision failed"})
		return
	}

	b.mu.Lock()
	instance.BoshTaskID = taskID
	b.mu.Unlock()

	resp := DeprovisionResponse{
		Operation: fmt.Sprintf("deprovision-%s", instanceID),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(resp)
}
