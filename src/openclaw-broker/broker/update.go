package broker

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/bosh"
)

type UpdateRequest struct {
	ServiceID  string                 `json:"service_id"`
	PlanID     string                 `json:"plan_id,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
}

func (b *Broker) Update(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	instanceID := vars["instance_id"]

	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error": "Bad request"}`, http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	instance, exists := b.instances[instanceID]
	if !exists {
		http.Error(w, `{"error": "Instance not found"}`, http.StatusNotFound)
		return
	}

	if req.PlanID != "" && req.PlanID != instance.PlanID {
		plan := b.findPlan(req.PlanID)
		if plan == nil {
			http.Error(w, `{"error": "Unknown plan"}`, http.StatusBadRequest)
			return
		}

		instance.PlanID = req.PlanID
		instance.PlanName = plan.Name
		instance.VMType = plan.VMType
		instance.DiskType = plan.DiskType

		params := b.buildManifestParams(instance)
		manifest, err := bosh.RenderAgentManifest(params)
		if err != nil {
			log.Printf("Manifest render failed for update %s: %v", instanceID, err)
			http.Error(w, `{"error": "Failed to render deployment manifest"}`, http.StatusInternalServerError)
			return
		}
		taskID, err := b.director.Deploy(manifest)
		if err != nil {
			http.Error(w, `{"error": "Update deployment failed"}`, http.StatusInternalServerError)
			return
		}
		instance.State = "provisioning"
		instance.BoshTaskID = taskID
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{"operation": "update-" + instanceID})
}
