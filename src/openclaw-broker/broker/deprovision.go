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

	b.mu.Lock()
	defer b.mu.Unlock()

	instance, exists := b.instances[instanceID]
	if !exists {
		w.WriteHeader(http.StatusGone)
		json.NewEncoder(w).Encode(map[string]string{})
		return
	}

	taskID, err := b.director.DeleteDeployment(instance.DeploymentName)
	if err != nil {
		log.Printf("BOSH delete failed for %s: %v", instanceID, err)
		http.Error(w, `{"error": "Deprovision failed"}`, http.StatusInternalServerError)
		return
	}

	instance.State = "deprovisioning"
	instance.BoshTaskID = taskID

	resp := DeprovisionResponse{
		Operation: fmt.Sprintf("deprovision-%s", instanceID),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(resp)
}
