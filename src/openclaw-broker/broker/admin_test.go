package broker

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/bosh"
)

// newTestBrokerWithAdminRoutes is like newTestBroker but also registers admin routes.
func newTestBrokerWithAdminRoutes(taskState string, deployFail bool) (*Broker, *httptest.Server, *mux.Router) {
	b, fakeBOSH, r := newTestBroker(taskState, deployFail)
	r.HandleFunc("/admin/instances", b.AdminListInstances).Methods("GET")
	r.HandleFunc("/admin/upgrade", b.AdminUpgrade).Methods("POST")
	r.HandleFunc("/admin/upgrade/status", b.AdminUpgradeStatus).Methods("GET")
	return b, fakeBOSH, r
}

func TestAdminListInstances_Empty(t *testing.T) {
	_, fakeBOSH, router := newTestBrokerWithAdminRoutes("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("GET", "/admin/instances", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var list []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("Expected empty list, got %d items", len(list))
	}
}

func TestAdminListInstances_ReturnsInstances(t *testing.T) {
	_, fakeBOSH, router := newTestBrokerWithAdminRoutes("done", false)
	defer fakeBOSH.Close()

	// Provision two instances
	provisionInstance(t, router, "inst-a", "openclaw-developer-plan")
	provisionInstance(t, router, "inst-b", "openclaw-team-plan")

	req := httptest.NewRequest("GET", "/admin/instances", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var list []map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("Expected 2 instances, got %d", len(list))
	}

	// Verify each entry has the required "id" field
	ids := map[string]bool{}
	for _, item := range list {
		id, ok := item["id"].(string)
		if !ok || id == "" {
			t.Error("Instance missing 'id' field")
		}
		ids[id] = true

		// Verify other expected fields are present
		for _, key := range []string{"deployment_name", "state", "openclaw_version", "plan_name", "owner"} {
			if _, ok := item[key]; !ok {
				t.Errorf("Instance %s missing field %q", id, key)
			}
		}
	}
	if !ids["inst-a"] || !ids["inst-b"] {
		t.Errorf("Expected inst-a and inst-b in list, got %v", ids)
	}

	// Verify the errand's json_array_len approach works (counts "id" occurrences)
	body := rr.Body.String()
	count := 0
	for i := 0; i < len(body)-3; i++ {
		if body[i:i+4] == `"id"` {
			count++
		}
	}
	if count != 2 {
		t.Errorf("json_array_len would count %d 'id' fields, want 2", count)
	}
}

func TestAdminListInstances_ExcludesDeprovisioning(t *testing.T) {
	b, fakeBOSH, router := newTestBrokerWithAdminRoutes("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-active", "openclaw-developer-plan")
	provisionInstance(t, router, "inst-dying", "openclaw-developer-plan")

	b.mu.Lock()
	b.instances["inst-dying"].State = "deprovisioning"
	b.mu.Unlock()

	req := httptest.NewRequest("GET", "/admin/instances", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var list []map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("Expected 1 instance (deprovisioning excluded), got %d", len(list))
	}
	if list[0]["id"] != "inst-active" {
		t.Errorf("Expected inst-active, got %v", list[0]["id"])
	}
}

func TestAdminUpgrade_UpgradesOutdatedInstances(t *testing.T) {
	b, fakeBOSH, router := newTestBrokerWithAdminRoutes("done", false)
	defer fakeBOSH.Close()

	// Provision instances then set them to ready with old version
	provisionInstance(t, router, "inst-old-1", "openclaw-developer-plan")
	provisionInstance(t, router, "inst-old-2", "openclaw-developer-plan")

	b.mu.Lock()
	b.instances["inst-old-1"].State = "ready"
	b.instances["inst-old-1"].OpenClawVersion = "2026.2.17"
	b.instances["inst-old-2"].State = "ready"
	b.instances["inst-old-2"].OpenClawVersion = "2026.2.17"
	b.mu.Unlock()

	body, _ := json.Marshal(map[string]interface{}{
		"target_version": "2026.2.21-2",
		"count":          2,
		"max_parallel":   5,
	})
	req := httptest.NewRequest("POST", "/admin/upgrade", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. Body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp map[string]int
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["upgrading"] != 2 {
		t.Errorf("upgrading = %d, want 2", resp["upgrading"])
	}

	// Verify instances were updated
	b.mu.RLock()
	for _, id := range []string{"inst-old-1", "inst-old-2"} {
		inst := b.instances[id]
		if inst.State != "provisioning" {
			t.Errorf("%s state = %q, want provisioning", id, inst.State)
		}
		if inst.OpenClawVersion != "2026.2.21-2" {
			t.Errorf("%s version = %q, want 2026.2.21-2", id, inst.OpenClawVersion)
		}
	}
	b.mu.RUnlock()

	// Verify upgrade tracker has entries
	b.upgrades.mu.Lock()
	if len(b.upgrades.tasks) != 2 {
		t.Errorf("upgrade tracker has %d entries, want 2", len(b.upgrades.tasks))
	}
	b.upgrades.mu.Unlock()
}

func TestAdminUpgrade_SkipsCurrentVersion(t *testing.T) {
	b, fakeBOSH, router := newTestBrokerWithAdminRoutes("done", false)
	defer fakeBOSH.Close()

	// Provision instance already at current version
	provisionInstance(t, router, "inst-current", "openclaw-developer-plan")
	b.mu.Lock()
	b.instances["inst-current"].State = "ready"
	// OpenClawVersion is already set to config version by provision
	b.mu.Unlock()

	body, _ := json.Marshal(map[string]interface{}{
		"target_version": "2026.2.21-2",
		"count":          1,
		"max_parallel":   5,
	})
	req := httptest.NewRequest("POST", "/admin/upgrade", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp map[string]int
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["upgrading"] != 0 {
		t.Errorf("upgrading = %d, want 0 (instance already current)", resp["upgrading"])
	}
}

func TestAdminUpgrade_RespectsCount(t *testing.T) {
	b, fakeBOSH, router := newTestBrokerWithAdminRoutes("done", false)
	defer fakeBOSH.Close()

	// Provision 3 old instances
	for i := 1; i <= 3; i++ {
		id := "inst-batch-" + string(rune('0'+i))
		provisionInstance(t, router, id, "openclaw-developer-plan")
		b.mu.Lock()
		b.instances[id].State = "ready"
		b.instances[id].OpenClawVersion = "2026.2.17"
		b.mu.Unlock()
	}

	// Request only 1
	body, _ := json.Marshal(map[string]interface{}{
		"target_version": "2026.2.21-2",
		"count":          1,
		"max_parallel":   5,
	})
	req := httptest.NewRequest("POST", "/admin/upgrade", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp map[string]int
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["upgrading"] != 1 {
		t.Errorf("upgrading = %d, want 1", resp["upgrading"])
	}
}

func TestAdminUpgrade_InvalidRequest(t *testing.T) {
	_, fakeBOSH, router := newTestBrokerWithAdminRoutes("done", false)
	defer fakeBOSH.Close()

	// Bad JSON
	req := httptest.NewRequest("POST", "/admin/upgrade", bytes.NewReader([]byte("{")))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("bad JSON status = %d, want %d", rr.Code, http.StatusBadRequest)
	}

	// count=0
	body, _ := json.Marshal(map[string]interface{}{
		"target_version": "2026.2.21-2",
		"count":          0,
		"max_parallel":   5,
	})
	req = httptest.NewRequest("POST", "/admin/upgrade", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("zero count status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestAdminUpgradeStatus_ReportsHealthy(t *testing.T) {
	b, fakeBOSH, router := newTestBrokerWithAdminRoutes("done", false)
	defer fakeBOSH.Close()

	// Provision and manually set up upgrade tracking
	provisionInstance(t, router, "inst-status-1", "openclaw-developer-plan")
	provisionInstance(t, router, "inst-status-2", "openclaw-developer-plan")

	b.mu.Lock()
	b.instances["inst-status-1"].State = "provisioning"
	b.instances["inst-status-1"].BoshTaskID = 42
	b.instances["inst-status-2"].State = "provisioning"
	b.instances["inst-status-2"].BoshTaskID = 42
	b.mu.Unlock()

	b.upgrades.mu.Lock()
	b.upgrades.tasks = map[string]int{
		"inst-status-1": 42, // fake BOSH returns "done" for task 42
		"inst-status-2": 42,
	}
	b.upgrades.mu.Unlock()

	req := httptest.NewRequest("GET", "/admin/upgrade/status", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp map[string]int
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp["healthy"] != 2 {
		t.Errorf("healthy = %d, want 2", resp["healthy"])
	}
	if resp["total"] != 2 {
		t.Errorf("total = %d, want 2", resp["total"])
	}
	if resp["failed"] != 0 {
		t.Errorf("failed = %d, want 0", resp["failed"])
	}
	if resp["in_progress"] != 0 {
		t.Errorf("in_progress = %d, want 0", resp["in_progress"])
	}

	// Verify instances were transitioned to "ready"
	b.mu.RLock()
	for _, id := range []string{"inst-status-1", "inst-status-2"} {
		if b.instances[id].State != "ready" {
			t.Errorf("%s state = %q, want ready", id, b.instances[id].State)
		}
	}
	b.mu.RUnlock()
}

func TestAdminUpgradeStatus_InProgress(t *testing.T) {
	b, fakeBOSH, router := newTestBrokerWithAdminRoutes("processing", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-prog", "openclaw-developer-plan")

	b.upgrades.mu.Lock()
	b.upgrades.tasks = map[string]int{
		"inst-prog": 42, // fake BOSH returns "processing"
	}
	b.upgrades.mu.Unlock()

	req := httptest.NewRequest("GET", "/admin/upgrade/status", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp map[string]int
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp["healthy"] != 0 {
		t.Errorf("healthy = %d, want 0", resp["healthy"])
	}
	if resp["in_progress"] != 1 {
		t.Errorf("in_progress = %d, want 1", resp["in_progress"])
	}
}

func TestAdminUpgradeStatus_Failed(t *testing.T) {
	b, fakeBOSH, router := newTestBrokerWithAdminRoutes("error", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-fail", "openclaw-developer-plan")

	b.upgrades.mu.Lock()
	b.upgrades.tasks = map[string]int{
		"inst-fail": 42, // fake BOSH returns "error"
	}
	b.upgrades.mu.Unlock()

	req := httptest.NewRequest("GET", "/admin/upgrade/status", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp map[string]int
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp["failed"] != 1 {
		t.Errorf("failed = %d, want 1", resp["failed"])
	}
	if resp["healthy"] != 0 {
		t.Errorf("healthy = %d, want 0", resp["healthy"])
	}

	// Verify instance state set to "failed"
	b.mu.RLock()
	if b.instances["inst-fail"].State != "failed" {
		t.Errorf("state = %q, want failed", b.instances["inst-fail"].State)
	}
	b.mu.RUnlock()
}

func TestAdminUpgradeStatus_Empty(t *testing.T) {
	_, fakeBOSH, router := newTestBrokerWithAdminRoutes("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("GET", "/admin/upgrade/status", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp map[string]int
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp["total"] != 0 {
		t.Errorf("total = %d, want 0", resp["total"])
	}
}

func TestAdminUpgrade_EndToEnd(t *testing.T) {
	// Full flow: provision old instances → upgrade → check status
	fakeBOSH := newFakeBOSHDirector("done", false)
	defer fakeBOSH.Close()

	director := bosh.NewClient(fakeBOSH.URL, "admin", "admin", "", "")
	cfg := BrokerConfig{
		OpenClawVersion: "2026.2.21-2",
		AZs:             []string{"z1"},
		AppsDomain:      "apps.example.com",
	}
	b := New(cfg, director)

	r := mux.NewRouter()
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Provision).Methods("PUT")
	r.HandleFunc("/admin/instances", b.AdminListInstances).Methods("GET")
	r.HandleFunc("/admin/upgrade", b.AdminUpgrade).Methods("POST")
	r.HandleFunc("/admin/upgrade/status", b.AdminUpgradeStatus).Methods("GET")

	// Provision 3 instances
	provisionInstance(t, r, "e2e-1", "openclaw-developer-plan")
	provisionInstance(t, r, "e2e-2", "openclaw-developer-plan")
	provisionInstance(t, r, "e2e-3", "openclaw-developer-plan")

	// Simulate them being at an older version
	b.mu.Lock()
	for _, id := range []string{"e2e-1", "e2e-2", "e2e-3"} {
		b.instances[id].State = "ready"
		b.instances[id].OpenClawVersion = "2026.2.17"
	}
	b.mu.Unlock()

	// List instances
	req := httptest.NewRequest("GET", "/admin/instances", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	var list []map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list) != 3 {
		t.Fatalf("Expected 3 instances, got %d", len(list))
	}

	// Upgrade canary (1 instance)
	body, _ := json.Marshal(map[string]interface{}{
		"target_version": "2026.2.21-2",
		"count":          1,
		"max_parallel":   5,
	})
	req = httptest.NewRequest("POST", "/admin/upgrade", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	var upgradeResp map[string]int
	json.Unmarshal(rr.Body.Bytes(), &upgradeResp)
	if upgradeResp["upgrading"] != 1 {
		t.Fatalf("canary upgrading = %d, want 1", upgradeResp["upgrading"])
	}

	// Check status — should show 1 healthy (BOSH returns "done")
	req = httptest.NewRequest("GET", "/admin/upgrade/status", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	var statusResp map[string]int
	json.Unmarshal(rr.Body.Bytes(), &statusResp)
	if statusResp["healthy"] != 1 {
		t.Errorf("canary healthy = %d, want 1", statusResp["healthy"])
	}

	// Upgrade remaining (2 instances)
	body, _ = json.Marshal(map[string]interface{}{
		"target_version": "2026.2.21-2",
		"count":          2,
		"max_parallel":   5,
	})
	req = httptest.NewRequest("POST", "/admin/upgrade", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	json.Unmarshal(rr.Body.Bytes(), &upgradeResp)
	if upgradeResp["upgrading"] != 2 {
		t.Fatalf("rolling upgrading = %d, want 2", upgradeResp["upgrading"])
	}

	// Final status check — all 3 should be healthy
	req = httptest.NewRequest("GET", "/admin/upgrade/status", nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	json.Unmarshal(rr.Body.Bytes(), &statusResp)
	if statusResp["healthy"] != 3 {
		t.Errorf("final healthy = %d, want 3", statusResp["healthy"])
	}
	if statusResp["total"] != 3 {
		t.Errorf("final total = %d, want 3", statusResp["total"])
	}
}
