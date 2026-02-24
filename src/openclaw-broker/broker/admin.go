package broker

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/bosh"
)

// AdminListInstances returns all known instances as a JSON array.
// The errand's json_array_len counts "id" fields, so each entry must include "id".
func (b *Broker) AdminListInstances(w http.ResponseWriter, r *http.Request) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	type instanceInfo struct {
		ID              string `json:"id"`
		DeploymentName  string `json:"deployment_name"`
		State           string `json:"state"`
		OpenClawVersion string `json:"openclaw_version"`
		PlanName        string `json:"plan_name"`
		Owner           string `json:"owner"`
	}

	list := make([]instanceInfo, 0, len(b.instances))
	for _, inst := range b.instances {
		if inst.State == "deprovisioning" {
			continue
		}
		list = append(list, instanceInfo{
			ID:              inst.ID,
			DeploymentName:  inst.DeploymentName,
			State:           inst.State,
			OpenClawVersion: inst.OpenClawVersion,
			PlanName:        inst.PlanName,
			Owner:           inst.Owner,
		})
	}

	writeJSON(w, http.StatusOK, list)
}

// AdminUpgrade triggers BOSH redeploys for instances that need upgrading.
// Accepts {"target_version": "...", "count": N, "max_parallel": N}.
// Picks up to count instances whose version differs from the broker's configured version.
func (b *Broker) AdminUpgrade(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetVersion string `json:"target_version"`
		Count         int    `json:"count"`
		MaxParallel   int    `json:"max_parallel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Bad request"})
		return
	}
	if req.Count <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "count must be positive"})
		return
	}

	configVersion := b.config.OpenClawVersion

	// Collect instances that need upgrading
	b.mu.Lock()
	var candidates []*Instance
	for _, inst := range b.instances {
		if inst.State == "deprovisioning" {
			continue
		}
		if inst.OpenClawVersion != configVersion {
			candidates = append(candidates, inst)
		}
		if len(candidates) >= req.Count {
			break
		}
	}
	b.mu.Unlock()

	upgraded := 0
	for _, inst := range candidates {
		b.mu.RLock()
		params := b.buildManifestParams(inst)
		b.mu.RUnlock()

		manifest, err := bosh.RenderAgentManifest(params)
		if err != nil {
			log.Printf("Upgrade manifest render failed for %s: %v", inst.ID, err)
			continue
		}
		taskID, err := b.director.Deploy(manifest)
		if err != nil {
			log.Printf("Upgrade deploy failed for %s: %v", inst.ID, err)
			continue
		}

		b.mu.Lock()
		inst.State = "provisioning"
		inst.BoshTaskID = taskID
		inst.OpenClawVersion = configVersion
		b.mu.Unlock()

		b.upgrades.mu.Lock()
		if b.upgrades.tasks == nil {
			b.upgrades.tasks = make(map[string]int)
		}
		b.upgrades.tasks[inst.ID] = taskID
		b.upgrades.mu.Unlock()

		upgraded++
		log.Printf("Upgrade started for %s: task=%d", inst.ID, taskID)
	}

	b.saveState()
	writeJSON(w, http.StatusOK, map[string]int{"upgrading": upgraded})
}

// AdminUpgradeStatus polls BOSH task status for tracked upgrades and returns counts.
// Returns {"healthy": N, "total": N, "failed": N, "in_progress": N}.
func (b *Broker) AdminUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	b.upgrades.mu.Lock()
	// Copy task map so we can release the lock before making BOSH calls
	tasks := make(map[string]int, len(b.upgrades.tasks))
	for id, tid := range b.upgrades.tasks {
		tasks[id] = tid
	}
	b.upgrades.mu.Unlock()

	total := len(tasks)
	healthy := 0
	failed := 0
	inProgress := 0

	for instID, taskID := range tasks {
		state, err := b.director.TaskStatus(taskID)
		if err != nil {
			log.Printf("Upgrade status check failed for %s (task %d): %v", instID, taskID, err)
			failed++
			continue
		}

		switch state {
		case "done":
			healthy++
			b.mu.Lock()
			if inst, ok := b.instances[instID]; ok {
				inst.State = "ready"
			}
			b.mu.Unlock()
		case "error", "cancelled":
			failed++
			b.mu.Lock()
			if inst, ok := b.instances[instID]; ok {
				inst.State = "failed"
			}
			b.mu.Unlock()
		default:
			inProgress++
		}
	}

	b.saveState()
	writeJSON(w, http.StatusOK, map[string]int{
		"healthy":     healthy,
		"total":       total,
		"failed":      failed,
		"in_progress": inProgress,
	})
}
