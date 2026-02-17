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

	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Bad request"})
		return
	}

	b.mu.Lock()

	instance, exists := b.instances[instanceID]
	if !exists {
		b.mu.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Instance not found"})
		return
	}

	// If the plan is changing, validate the new plan and update instance fields
	planChanged := req.PlanID != "" && req.PlanID != instance.PlanID
	if planChanged {
		plan := b.findPlan(req.PlanID)
		if plan == nil {
			b.mu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Unknown plan"})
			return
		}
		instance.PlanID = req.PlanID
		instance.PlanName = plan.Name
		instance.VMType = plan.VMType
		instance.DiskType = plan.DiskType
	}

	// Always redeploy — this ensures instances pick up new release versions
	// after a tile update (e.g., gateway code fixes, security patches).
	params := b.buildManifestParams(instance)
	b.mu.Unlock()

	manifest, err := bosh.RenderAgentManifest(params)
	if err != nil {
		log.Printf("Manifest render failed for update %s: %v", instanceID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to render deployment manifest"})
		return
	}
	taskID, err := b.director.Deploy(manifest)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Update deployment failed"})
		return
	}

	b.mu.Lock()
	instance.State = "provisioning"
	instance.BoshTaskID = taskID
	b.mu.Unlock()

	writeJSON(w, http.StatusAccepted, map[string]string{"operation": "update-" + instanceID})
}
