package broker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/nkuhn-vmw/bosh-openclaw/src/openclaw-broker/bosh"
)

// --- helpers ---

// newFakeBOSHDirector creates an httptest.Server that simulates the BOSH Director API.
// taskState controls what TaskStatus returns. deployFail causes Deploy to return 500.
// Deploy and DeleteDeployment return 200 OK with a Location header containing the task
// path, since Go's HTTP client follows 302 redirects by default which would lose the
// Location header. The BOSH client accepts both 200 and 302 for deploy responses.
func newFakeBOSHDirector(taskState string, deployFail bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// POST /deployments -> Deploy
		case r.Method == "POST" && r.URL.Path == "/deployments":
			if deployFail {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("deploy error"))
				return
			}
			w.Header().Set("Location", "/tasks/42")
			w.WriteHeader(http.StatusOK)

		// DELETE /deployments/{name} -> DeleteDeployment
		case r.Method == "DELETE" && len(r.URL.Path) > len("/deployments/"):
			w.Header().Set("Location", "/tasks/99")
			w.WriteHeader(http.StatusOK)

		// GET /tasks/{id} -> TaskStatus
		case r.Method == "GET" && len(r.URL.Path) > len("/tasks/"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"state": taskState})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

// newTestBroker creates a Broker backed by a fake BOSH director.
// Returns the broker, the fake BOSH server (caller should defer Close()), and a mux.Router wired up the same as main.go.
func newTestBroker(taskState string, deployFail bool) (*Broker, *httptest.Server, *mux.Router) {
	fakeBOSH := newFakeBOSHDirector(taskState, deployFail)

	director := bosh.NewClient(fakeBOSH.URL, "admin", "admin", "", "")
	cfg := BrokerConfig{
		MinOpenClawVersion: "2026.1.29",
		ControlUIEnabled:   false,
		SandboxMode:        "strict",
		OpenClawVersion:    "2026.2.10",
	}
	b := New(cfg, director)

	r := mux.NewRouter()
	r.HandleFunc("/v2/catalog", b.Catalog).Methods("GET")
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Provision).Methods("PUT")
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Deprovision).Methods("DELETE")
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Update).Methods("PATCH")
	r.HandleFunc("/v2/service_instances/{instance_id}/service_bindings/{binding_id}", b.Bind).Methods("PUT")
	r.HandleFunc("/v2/service_instances/{instance_id}/service_bindings/{binding_id}", b.Unbind).Methods("DELETE")
	r.HandleFunc("/v2/service_instances/{instance_id}/last_operation", b.LastOperation).Methods("GET")

	return b, fakeBOSH, r
}

// provisionInstance is a helper that provisions a valid instance and returns the HTTP response.
func provisionInstance(t *testing.T, router *mux.Router, instanceID, planID string) *httptest.ResponseRecorder {
	t.Helper()
	body := ProvisionRequest{
		ServiceID:        "openclaw-service",
		PlanID:           planID,
		OrganizationGUID: "org-123",
		SpaceGUID:        "space-456",
		Parameters: map[string]interface{}{
			"owner":            "dev@example.com",
			"openclaw_version": "2026.2.10",
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/v2/service_instances/"+instanceID, bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

// --- Catalog tests ---

func TestCatalog_ReturnsJSON(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("GET", "/v2/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Catalog() status = %d, want %d", rr.Code, http.StatusOK)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestCatalog_HasOneService(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("GET", "/v2/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var catalog CatalogResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("Failed to unmarshal catalog: %v", err)
	}

	if len(catalog.Services) != 1 {
		t.Fatalf("Catalog has %d services, want 1", len(catalog.Services))
	}
	if catalog.Services[0].Name != "openclaw" {
		t.Errorf("Service name = %q, want %q", catalog.Services[0].Name, "openclaw")
	}
}

func TestCatalog_HasThreePlans(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("GET", "/v2/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var catalog CatalogResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("Failed to unmarshal catalog: %v", err)
	}

	plans := catalog.Services[0].Plans
	if len(plans) != 3 {
		t.Fatalf("Catalog has %d plans, want 3", len(plans))
	}

	expectedPlanIDs := map[string]bool{
		"openclaw-developer-plan":      false,
		"openclaw-developer-plus-plan": false,
		"openclaw-team-plan":           false,
	}
	for _, p := range plans {
		if _, ok := expectedPlanIDs[p.ID]; !ok {
			t.Errorf("Unexpected plan ID: %q", p.ID)
		}
		expectedPlanIDs[p.ID] = true
	}
	for id, found := range expectedPlanIDs {
		if !found {
			t.Errorf("Expected plan ID %q not found in catalog", id)
		}
	}
}

func TestCatalog_ServiceIsBindable(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("GET", "/v2/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var catalog CatalogResponse
	json.Unmarshal(rr.Body.Bytes(), &catalog)

	svc := catalog.Services[0]
	if !svc.Bindable {
		t.Error("Service.Bindable = false, want true")
	}
	if !svc.PlanUpdatable {
		t.Error("Service.PlanUpdatable = false, want true")
	}
}

func TestCatalog_ServiceHasTags(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("GET", "/v2/catalog", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var catalog CatalogResponse
	json.Unmarshal(rr.Body.Bytes(), &catalog)

	tags := catalog.Services[0].Tags
	expectedTags := []string{"ai", "agent", "openclaw", "llm"}
	if len(tags) != len(expectedTags) {
		t.Fatalf("Tags count = %d, want %d", len(tags), len(expectedTags))
	}
	for i, tag := range tags {
		if tag != expectedTags[i] {
			t.Errorf("Tag[%d] = %q, want %q", i, tag, expectedTags[i])
		}
	}
}

func TestCatalog_UsesConfigPlans(t *testing.T) {
	fakeBOSH := newFakeBOSHDirector("done", false)
	defer fakeBOSH.Close()

	director := bosh.NewClient(fakeBOSH.URL, "admin", "admin", "", "")
	cfg := BrokerConfig{
		OpenClawVersion: "2026.2.10",
		Plans: []Plan{
			{ID: "custom-plan-1", Name: "custom", Description: "Custom plan", VMType: "tiny", DiskType: "5GB"},
			{ID: "custom-plan-2", Name: "custom-big", Description: "Big custom plan", VMType: "huge", DiskType: "100GB"},
		},
	}
	b := New(cfg, director)

	r := mux.NewRouter()
	r.HandleFunc("/v2/catalog", b.Catalog).Methods("GET")

	req := httptest.NewRequest("GET", "/v2/catalog", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	var catalog CatalogResponse
	json.Unmarshal(rr.Body.Bytes(), &catalog)

	plans := catalog.Services[0].Plans
	if len(plans) != 2 {
		t.Fatalf("Catalog has %d plans, want 2", len(plans))
	}
	if plans[0].ID != "custom-plan-1" {
		t.Errorf("Plan[0].ID = %q, want %q", plans[0].ID, "custom-plan-1")
	}
	if plans[1].ID != "custom-plan-2" {
		t.Errorf("Plan[1].ID = %q, want %q", plans[1].ID, "custom-plan-2")
	}
}

// --- Provision tests ---

func TestProvision_Success(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	rr := provisionInstance(t, router, "inst-001", "openclaw-developer-plan")

	if rr.Code != http.StatusAccepted {
		t.Fatalf("Provision() status = %d, want %d. Body: %s", rr.Code, http.StatusAccepted, rr.Body.String())
	}

	var resp ProvisionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if resp.DashboardURL == "" {
		t.Error("DashboardURL is empty")
	}
	if resp.Operation == "" {
		t.Error("Operation is empty")
	}
}

func TestProvision_DuplicateInstance(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	// First provision
	rr := provisionInstance(t, router, "inst-dup", "openclaw-developer-plan")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("First provision failed: %d", rr.Code)
	}

	// Second provision of same ID
	rr = provisionInstance(t, router, "inst-dup", "openclaw-developer-plan")
	if rr.Code != http.StatusConflict {
		t.Errorf("Duplicate provision status = %d, want %d", rr.Code, http.StatusConflict)
	}
}

func TestProvision_UnknownPlan(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	body := ProvisionRequest{
		ServiceID:        "openclaw-service",
		PlanID:           "nonexistent-plan",
		OrganizationGUID: "org-123",
		SpaceGUID:        "space-456",
		Parameters: map[string]interface{}{
			"openclaw_version": "2026.2.10",
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/v2/service_instances/inst-bad-plan", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Unknown plan status = %d, want %d. Body: %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestProvision_RejectsVersionBelowMinimum(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	body := ProvisionRequest{
		ServiceID:        "openclaw-service",
		PlanID:           "openclaw-developer-plan",
		OrganizationGUID: "org-123",
		SpaceGUID:        "space-456",
		Parameters: map[string]interface{}{
			"openclaw_version": "2025.1.1",
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/v2/service_instances/inst-old-ver", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("Old version status = %d, want %d. Body: %s", rr.Code, http.StatusUnprocessableEntity, rr.Body.String())
	}
}

func TestProvision_AcceptsVersionAtMinimum(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	body := ProvisionRequest{
		ServiceID:        "openclaw-service",
		PlanID:           "openclaw-developer-plan",
		OrganizationGUID: "org-123",
		SpaceGUID:        "space-456",
		Parameters: map[string]interface{}{
			"openclaw_version": "2026.1.29",
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/v2/service_instances/inst-min-ver", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("Minimum version status = %d, want %d. Body: %s", rr.Code, http.StatusAccepted, rr.Body.String())
	}
}

func TestProvision_BOSHDeployFailure(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", true) // deployFail=true
	defer fakeBOSH.Close()

	body := ProvisionRequest{
		ServiceID:        "openclaw-service",
		PlanID:           "openclaw-developer-plan",
		OrganizationGUID: "org-123",
		SpaceGUID:        "space-456",
		Parameters: map[string]interface{}{
			"openclaw_version": "2026.2.10",
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/v2/service_instances/inst-bosh-fail", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("BOSH failure status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestProvision_InvalidJSON(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("PUT", "/v2/service_instances/inst-bad-json", bytes.NewReader([]byte("not-json")))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Invalid JSON status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestProvision_AllPlans(t *testing.T) {
	plans := []struct {
		planID   string
		planName string
	}{
		{"openclaw-developer-plan", "developer"},
		{"openclaw-developer-plus-plan", "developer-plus"},
		{"openclaw-team-plan", "team"},
	}

	for _, p := range plans {
		t.Run(p.planName, func(t *testing.T) {
			_, fakeBOSH, router := newTestBroker("done", false)
			defer fakeBOSH.Close()

			rr := provisionInstance(t, router, "inst-"+p.planName, p.planID)
			if rr.Code != http.StatusAccepted {
				t.Errorf("Plan %q provision status = %d, want %d", p.planName, rr.Code, http.StatusAccepted)
			}
		})
	}
}

func TestProvision_SetsCorrectState(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	rr := provisionInstance(t, router, "inst-state", "openclaw-developer-plan")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("Provision failed: %d", rr.Code)
	}

	b.mu.RLock()
	inst, exists := b.instances["inst-state"]
	b.mu.RUnlock()

	if !exists {
		t.Fatal("Instance not found after provision")
	}
	if inst.State != "provisioning" {
		t.Errorf("Instance state = %q, want %q", inst.State, "provisioning")
	}
	if inst.BoshTaskID != 42 {
		t.Errorf("BoshTaskID = %d, want 42", inst.BoshTaskID)
	}
}

func TestProvision_SetsOwnerFromParameters(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-owner", "openclaw-developer-plan")

	b.mu.RLock()
	inst := b.instances["inst-owner"]
	b.mu.RUnlock()

	if inst.Owner != "dev@example.com" {
		t.Errorf("Owner = %q, want %q", inst.Owner, "dev@example.com")
	}
}

func TestProvision_SetsVMTypeAndDiskTypeFromPlan(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-vm", "openclaw-developer-plan")

	b.mu.RLock()
	inst := b.instances["inst-vm"]
	b.mu.RUnlock()

	if inst.VMType != "small" {
		t.Errorf("VMType = %q, want %q", inst.VMType, "small")
	}
	if inst.DiskType != "10GB" {
		t.Errorf("DiskType = %q, want %q", inst.DiskType, "10GB")
	}
}

func TestProvision_UsesConfigVersionWhenNotInParams(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	body := ProvisionRequest{
		ServiceID:        "openclaw-service",
		PlanID:           "openclaw-developer-plan",
		OrganizationGUID: "org-123",
		SpaceGUID:        "space-456",
		Parameters:       map[string]interface{}{},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/v2/service_instances/inst-cfg-ver", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("Provision failed: %d, body: %s", rr.Code, rr.Body.String())
	}

	b.mu.RLock()
	inst := b.instances["inst-cfg-ver"]
	b.mu.RUnlock()

	if inst.OpenClawVersion != "2026.2.10" {
		t.Errorf("OpenClawVersion = %q, want %q", inst.OpenClawVersion, "2026.2.10")
	}
}

// --- Deprovision tests ---

func TestDeprovision_ExistingInstance(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	// First provision
	provisionInstance(t, router, "inst-deprov", "openclaw-developer-plan")

	// Then deprovision
	req := httptest.NewRequest("DELETE", "/v2/service_instances/inst-deprov?service_id=openclaw-service&plan_id=openclaw-developer-plan", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("Deprovision status = %d, want %d. Body: %s", rr.Code, http.StatusAccepted, rr.Body.String())
	}

	var resp DeprovisionResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Operation == "" {
		t.Error("Deprovision operation is empty")
	}
}

func TestDeprovision_NonExistentInstance(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("DELETE", "/v2/service_instances/inst-ghost?service_id=openclaw-service&plan_id=openclaw-developer-plan", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusGone {
		t.Errorf("Deprovision missing instance status = %d, want %d", rr.Code, http.StatusGone)
	}
}

func TestDeprovision_SetsStateToDeprovisioning(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-deprov-state", "openclaw-developer-plan")

	req := httptest.NewRequest("DELETE", "/v2/service_instances/inst-deprov-state?service_id=openclaw-service&plan_id=openclaw-developer-plan", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("Deprovision failed: %d", rr.Code)
	}

	b.mu.RLock()
	inst := b.instances["inst-deprov-state"]
	b.mu.RUnlock()

	if inst.State != "deprovisioning" {
		t.Errorf("State = %q, want %q", inst.State, "deprovisioning")
	}
	if inst.BoshTaskID != 99 {
		t.Errorf("BoshTaskID = %d, want 99", inst.BoshTaskID)
	}
}

// --- Bind tests ---

func TestBind_ReadyInstance(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-bind", "openclaw-developer-plan")

	// Manually set state to "ready"
	b.mu.Lock()
	b.instances["inst-bind"].State = "ready"
	b.instances["inst-bind"].AppsDomain = "apps.example.com"
	b.mu.Unlock()

	body := BindRequest{ServiceID: "openclaw-service", PlanID: "openclaw-developer-plan"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/v2/service_instances/inst-bind/service_bindings/bind-001", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("Bind status = %d, want %d. Body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}

	var resp BindResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	creds := resp.Credentials
	if creds["instance_id"] != "inst-bind" {
		t.Errorf("instance_id = %v, want %q", creds["instance_id"], "inst-bind")
	}
	if creds["gateway_token"] == nil || creds["gateway_token"] == "" {
		t.Error("gateway_token is missing or empty")
	}
	if creds["node_seed"] == nil || creds["node_seed"] == "" {
		t.Error("node_seed is missing or empty")
	}
	if creds["webchat_url"] == nil {
		t.Error("webchat_url is missing")
	}
	if creds["gateway_url"] == nil {
		t.Error("gateway_url is missing")
	}
	if creds["api_endpoint"] == nil {
		t.Error("api_endpoint is missing")
	}
	if creds["plan"] != "developer" {
		t.Errorf("plan = %v, want %q", creds["plan"], "developer")
	}
}

func TestBind_InstanceNotFound(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	body := BindRequest{ServiceID: "openclaw-service", PlanID: "openclaw-developer-plan"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/v2/service_instances/inst-missing/service_bindings/bind-001", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Bind missing instance status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestBind_InstanceNotReady(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	// Instance is in "provisioning" state after provision
	provisionInstance(t, router, "inst-not-ready", "openclaw-developer-plan")

	body := BindRequest{ServiceID: "openclaw-service", PlanID: "openclaw-developer-plan"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/v2/service_instances/inst-not-ready/service_bindings/bind-001", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("Bind not-ready instance status = %d, want %d", rr.Code, http.StatusUnprocessableEntity)
	}
}

func TestBind_CredentialsContainExpectedKeys(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-keys", "openclaw-team-plan")
	b.mu.Lock()
	b.instances["inst-keys"].State = "ready"
	b.mu.Unlock()

	body := BindRequest{ServiceID: "openclaw-service", PlanID: "openclaw-team-plan"}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PUT", "/v2/service_instances/inst-keys/service_bindings/bind-keys", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp BindResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	expectedKeys := []string{
		"webchat_url", "gateway_url", "gateway_token", "api_endpoint",
		"instance_id", "owner", "plan", "openclaw_version", "node_seed", "sso_enabled",
	}
	for _, key := range expectedKeys {
		if _, ok := resp.Credentials[key]; !ok {
			t.Errorf("Credentials missing key %q", key)
		}
	}
}

// --- Unbind tests ---

func TestUnbind_ReturnsOK(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("DELETE", "/v2/service_instances/inst-any/service_bindings/bind-any", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Unbind status = %d, want %d", rr.Code, http.StatusOK)
	}
}

// --- LastOperation tests ---

func TestLastOperation_ProvisioningDone(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-lo-done", "openclaw-developer-plan")

	req := httptest.NewRequest("GET", "/v2/service_instances/inst-lo-done/last_operation", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("LastOperation status = %d, want %d", rr.Code, http.StatusOK)
	}

	var resp LastOperationResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.State != "succeeded" {
		t.Errorf("LastOperation state = %q, want %q", resp.State, "succeeded")
	}
}

func TestLastOperation_ProvisioningInProgress(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("processing", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-lo-prog", "openclaw-developer-plan")

	req := httptest.NewRequest("GET", "/v2/service_instances/inst-lo-prog/last_operation", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp LastOperationResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.State != "in progress" {
		t.Errorf("LastOperation state = %q, want %q", resp.State, "in progress")
	}
}

func TestLastOperation_ProvisioningError(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("error", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-lo-err", "openclaw-developer-plan")

	req := httptest.NewRequest("GET", "/v2/service_instances/inst-lo-err/last_operation", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp LastOperationResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.State != "failed" {
		t.Errorf("LastOperation state = %q, want %q", resp.State, "failed")
	}
}

func TestLastOperation_ProvisioningCancelled(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("cancelled", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-lo-cancel", "openclaw-developer-plan")

	req := httptest.NewRequest("GET", "/v2/service_instances/inst-lo-cancel/last_operation", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp LastOperationResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.State != "failed" {
		t.Errorf("LastOperation state = %q, want %q", resp.State, "failed")
	}
}

func TestLastOperation_InstanceNotFound(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("GET", "/v2/service_instances/inst-nonexistent/last_operation", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("LastOperation missing instance status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestLastOperation_ReadyState(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-lo-ready", "openclaw-developer-plan")
	b.mu.Lock()
	b.instances["inst-lo-ready"].State = "ready"
	b.mu.Unlock()

	req := httptest.NewRequest("GET", "/v2/service_instances/inst-lo-ready/last_operation", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp LastOperationResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.State != "succeeded" {
		t.Errorf("LastOperation state = %q, want %q", resp.State, "succeeded")
	}
}

func TestLastOperation_FailedState(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-lo-failed", "openclaw-developer-plan")
	b.mu.Lock()
	b.instances["inst-lo-failed"].State = "failed"
	b.mu.Unlock()

	req := httptest.NewRequest("GET", "/v2/service_instances/inst-lo-failed/last_operation", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp LastOperationResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.State != "failed" {
		t.Errorf("LastOperation state = %q, want %q", resp.State, "failed")
	}
}

func TestLastOperation_DeprovisioningDone(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-lo-dep-done", "openclaw-developer-plan")

	// Simulate deprovisioning state
	b.mu.Lock()
	b.instances["inst-lo-dep-done"].State = "deprovisioning"
	b.instances["inst-lo-dep-done"].BoshTaskID = 99
	b.mu.Unlock()

	req := httptest.NewRequest("GET", "/v2/service_instances/inst-lo-dep-done/last_operation", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp LastOperationResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.State != "succeeded" {
		t.Errorf("LastOperation deprovisioning done state = %q, want %q", resp.State, "succeeded")
	}

	// Instance should be deleted
	b.mu.RLock()
	_, exists := b.instances["inst-lo-dep-done"]
	b.mu.RUnlock()
	if exists {
		t.Error("Instance should have been deleted after deprovisioning done")
	}
}

func TestLastOperation_DeprovisioningInProgress(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("processing", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-lo-dep-prog", "openclaw-developer-plan")
	b.mu.Lock()
	b.instances["inst-lo-dep-prog"].State = "deprovisioning"
	b.mu.Unlock()

	req := httptest.NewRequest("GET", "/v2/service_instances/inst-lo-dep-prog/last_operation", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp LastOperationResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.State != "in progress" {
		t.Errorf("LastOperation deprovisioning in progress state = %q, want %q", resp.State, "in progress")
	}
}

func TestLastOperation_DeprovisioningError(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("error", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-lo-dep-err", "openclaw-developer-plan")
	b.mu.Lock()
	b.instances["inst-lo-dep-err"].State = "deprovisioning"
	b.mu.Unlock()

	req := httptest.NewRequest("GET", "/v2/service_instances/inst-lo-dep-err/last_operation", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp LastOperationResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if resp.State != "failed" {
		t.Errorf("LastOperation deprovisioning error state = %q, want %q", resp.State, "failed")
	}
}

// --- Update tests ---

func TestUpdate_ChangePlan(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-update", "openclaw-developer-plan")

	body := UpdateRequest{
		ServiceID: "openclaw-service",
		PlanID:    "openclaw-team-plan",
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/v2/service_instances/inst-update", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("Update status = %d, want %d. Body: %s", rr.Code, http.StatusAccepted, rr.Body.String())
	}

	b.mu.RLock()
	inst := b.instances["inst-update"]
	b.mu.RUnlock()

	if inst.PlanID != "openclaw-team-plan" {
		t.Errorf("PlanID = %q, want %q", inst.PlanID, "openclaw-team-plan")
	}
	if inst.PlanName != "team" {
		t.Errorf("PlanName = %q, want %q", inst.PlanName, "team")
	}
	if inst.VMType != "large" {
		t.Errorf("VMType = %q, want %q after plan change", inst.VMType, "large")
	}
	if inst.DiskType != "50GB" {
		t.Errorf("DiskType = %q, want %q after plan change", inst.DiskType, "50GB")
	}
	if inst.State != "provisioning" {
		t.Errorf("State = %q, want %q after re-deploy", inst.State, "provisioning")
	}
}

func TestUpdate_SamePlanNoRedeploy(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-same-plan", "openclaw-developer-plan")

	// Set state to ready so we can check it is NOT reset to provisioning
	b.mu.Lock()
	b.instances["inst-same-plan"].State = "ready"
	b.mu.Unlock()

	body := UpdateRequest{
		ServiceID: "openclaw-service",
		PlanID:    "openclaw-developer-plan", // same plan
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/v2/service_instances/inst-same-plan", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("Update same plan status = %d, want %d", rr.Code, http.StatusAccepted)
	}

	b.mu.RLock()
	inst := b.instances["inst-same-plan"]
	b.mu.RUnlock()

	// Should remain "ready" since no re-deploy happened
	if inst.State != "ready" {
		t.Errorf("State = %q, want %q (no re-deploy for same plan)", inst.State, "ready")
	}
}

func TestUpdate_InstanceNotFound(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	body := UpdateRequest{
		ServiceID: "openclaw-service",
		PlanID:    "openclaw-team-plan",
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/v2/service_instances/inst-missing", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("Update missing instance status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestUpdate_InvalidJSON(t *testing.T) {
	_, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	req := httptest.NewRequest("PATCH", "/v2/service_instances/inst-bad", bytes.NewReader([]byte("{bad")))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Update bad JSON status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestUpdate_BOSHDeployFailure(t *testing.T) {
	// Create a BOSH server where deploy succeeds on first call but fails on second
	callCount := 0
	boshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/deployments":
			callCount++
			if callCount > 1 {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("deploy error"))
				return
			}
			w.Header().Set("Location", "/tasks/42")
			w.WriteHeader(http.StatusOK)
		case r.Method == "GET" && len(r.URL.Path) > len("/tasks/"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"state": "done"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer boshServer.Close()

	director := bosh.NewClient(boshServer.URL, "admin", "admin", "", "")
	cfg := BrokerConfig{
		MinOpenClawVersion: "2026.1.29",
		ControlUIEnabled:   false,
		SandboxMode:        "strict",
		OpenClawVersion:    "2026.2.10",
	}
	b := New(cfg, director)

	r := mux.NewRouter()
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Provision).Methods("PUT")
	r.HandleFunc("/v2/service_instances/{instance_id}", b.Update).Methods("PATCH")

	// Provision first (succeeds)
	provisionInstance(t, r, "inst-upd-fail", "openclaw-developer-plan")

	// Now update (deploy will fail on second call)
	body := UpdateRequest{
		ServiceID: "openclaw-service",
		PlanID:    "openclaw-team-plan",
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/v2/service_instances/inst-upd-fail", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("Update BOSH failure status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestUpdate_EmptyPlanIDNoRedeploy(t *testing.T) {
	b, fakeBOSH, router := newTestBroker("done", false)
	defer fakeBOSH.Close()

	provisionInstance(t, router, "inst-empty-plan", "openclaw-developer-plan")
	b.mu.Lock()
	b.instances["inst-empty-plan"].State = "ready"
	b.mu.Unlock()

	body := UpdateRequest{
		ServiceID: "openclaw-service",
		PlanID:    "", // empty plan ID
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/v2/service_instances/inst-empty-plan", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("Update empty plan status = %d, want %d", rr.Code, http.StatusAccepted)
	}

	b.mu.RLock()
	inst := b.instances["inst-empty-plan"]
	b.mu.RUnlock()

	if inst.State != "ready" {
		t.Errorf("State = %q, want %q (no re-deploy for empty plan)", inst.State, "ready")
	}
}

// --- sanitizeHostname tests ---

func TestSanitizeHostname_BasicEmail(t *testing.T) {
	result := sanitizeHostname("dev@example.com")
	if result != "dev" {
		t.Errorf("sanitizeHostname(%q) = %q, want %q", "dev@example.com", result, "dev")
	}
}

func TestSanitizeHostname_DotsReplacedWithHyphens(t *testing.T) {
	result := sanitizeHostname("first.last")
	if result != "first-last" {
		t.Errorf("sanitizeHostname(%q) = %q, want %q", "first.last", result, "first-last")
	}
}

func TestSanitizeHostname_UnderscoresReplacedWithHyphens(t *testing.T) {
	result := sanitizeHostname("first_last")
	if result != "first-last" {
		t.Errorf("sanitizeHostname(%q) = %q, want %q", "first_last", result, "first-last")
	}
}

func TestSanitizeHostname_UppercaseToLowercase(t *testing.T) {
	result := sanitizeHostname("DevUser")
	if result != "devuser" {
		t.Errorf("sanitizeHostname(%q) = %q, want %q", "DevUser", result, "devuser")
	}
}

func TestSanitizeHostname_EmailWithDotsAndUnderscores(t *testing.T) {
	result := sanitizeHostname("first.last_name@company.io")
	if result != "first-last-name" {
		t.Errorf("sanitizeHostname(%q) = %q, want %q", "first.last_name@company.io", result, "first-last-name")
	}
}

func TestSanitizeHostname_PlainString(t *testing.T) {
	result := sanitizeHostname("simple")
	if result != "simple" {
		t.Errorf("sanitizeHostname(%q) = %q, want %q", "simple", result, "simple")
	}
}

func TestSanitizeHostname_EmptyString(t *testing.T) {
	result := sanitizeHostname("")
	if result != "" {
		t.Errorf("sanitizeHostname(%q) = %q, want %q", "", result, "")
	}
}

// --- findPlan tests ---

func TestFindPlan_DeveloperPlan(t *testing.T) {
	b, fakeBOSH, _ := newTestBroker("done", false)
	defer fakeBOSH.Close()

	plan := b.findPlan("openclaw-developer-plan")
	if plan == nil {
		t.Fatal("findPlan returned nil for developer plan")
	}
	if plan.Name != "developer" {
		t.Errorf("plan.Name = %q, want %q", plan.Name, "developer")
	}
	if plan.VMType != "small" {
		t.Errorf("plan.VMType = %q, want %q", plan.VMType, "small")
	}
}

func TestFindPlan_DeveloperPlusPlan(t *testing.T) {
	b, fakeBOSH, _ := newTestBroker("done", false)
	defer fakeBOSH.Close()

	plan := b.findPlan("openclaw-developer-plus-plan")
	if plan == nil {
		t.Fatal("findPlan returned nil for developer-plus plan")
	}
	if plan.Name != "developer-plus" {
		t.Errorf("plan.Name = %q, want %q", plan.Name, "developer-plus")
	}
}

func TestFindPlan_TeamPlan(t *testing.T) {
	b, fakeBOSH, _ := newTestBroker("done", false)
	defer fakeBOSH.Close()

	plan := b.findPlan("openclaw-team-plan")
	if plan == nil {
		t.Fatal("findPlan returned nil for team plan")
	}
	if plan.Name != "team" {
		t.Errorf("plan.Name = %q, want %q", plan.Name, "team")
	}
}

func TestFindPlan_UnknownPlan(t *testing.T) {
	b, fakeBOSH, _ := newTestBroker("done", false)
	defer fakeBOSH.Close()

	plan := b.findPlan("unknown-plan")
	if plan != nil {
		t.Errorf("findPlan returned non-nil for unknown plan: %+v", plan)
	}
}

func TestFindPlan_UsesConfigPlans(t *testing.T) {
	fakeBOSH := newFakeBOSHDirector("done", false)
	defer fakeBOSH.Close()

	director := bosh.NewClient(fakeBOSH.URL, "admin", "admin", "", "")
	cfg := BrokerConfig{
		Plans: []Plan{
			{ID: "custom-plan", Name: "custom", VMType: "micro", DiskType: "1GB"},
		},
	}
	b := New(cfg, director)

	plan := b.findPlan("custom-plan")
	if plan == nil {
		t.Fatal("findPlan returned nil for custom config plan")
	}
	if plan.Name != "custom" {
		t.Errorf("plan.Name = %q, want %q", plan.Name, "custom")
	}

	// Default plans should not be found when config plans are set
	defaultPlan := b.findPlan("openclaw-developer-plan")
	if defaultPlan != nil {
		t.Error("findPlan should not return default plan when config plans are set")
	}
}

// --- New() constructor tests ---

func TestNew_ReturnsNonNil(t *testing.T) {
	fakeBOSH := newFakeBOSHDirector("done", false)
	defer fakeBOSH.Close()
	director := bosh.NewClient(fakeBOSH.URL, "admin", "admin", "", "")

	b := New(BrokerConfig{}, director)
	if b == nil {
		t.Fatal("New() returned nil")
	}
}

func TestNew_InitializesInstancesMap(t *testing.T) {
	fakeBOSH := newFakeBOSHDirector("done", false)
	defer fakeBOSH.Close()
	director := bosh.NewClient(fakeBOSH.URL, "admin", "admin", "", "")

	b := New(BrokerConfig{}, director)
	if b.instances == nil {
		t.Error("instances map is nil")
	}
	if len(b.instances) != 0 {
		t.Errorf("instances map length = %d, want 0", len(b.instances))
	}
}

// --- Integration-style test: full lifecycle ---

func TestFullLifecycle_ProvisionBindDeprovision(t *testing.T) {
	// Set up a BOSH server that transitions states
	taskState := "processing"
	boshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/deployments":
			w.Header().Set("Location", "/tasks/42")
			w.WriteHeader(http.StatusOK)
		case r.Method == "DELETE" && len(r.URL.Path) > len("/deployments/"):
			w.Header().Set("Location", "/tasks/99")
			w.WriteHeader(http.StatusOK)
		case r.Method == "GET" && len(r.URL.Path) > len("/tasks/"):
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"state": taskState})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer boshServer.Close()

	director := bosh.NewClient(boshServer.URL, "admin", "admin", "", "")
	cfg := BrokerConfig{
		MinOpenClawVersion: "2026.1.29",
		ControlUIEnabled:   false,
		SandboxMode:        "strict",
		OpenClawVersion:    "2026.2.10",
	}
	b := New(cfg, director)

	router := mux.NewRouter()
	router.HandleFunc("/v2/service_instances/{instance_id}", b.Provision).Methods("PUT")
	router.HandleFunc("/v2/service_instances/{instance_id}", b.Deprovision).Methods("DELETE")
	router.HandleFunc("/v2/service_instances/{instance_id}/service_bindings/{binding_id}", b.Bind).Methods("PUT")
	router.HandleFunc("/v2/service_instances/{instance_id}/service_bindings/{binding_id}", b.Unbind).Methods("DELETE")
	router.HandleFunc("/v2/service_instances/{instance_id}/last_operation", b.LastOperation).Methods("GET")

	instanceID := "lifecycle-001"

	// Step 1: Provision
	rr := provisionInstance(t, router, instanceID, "openclaw-developer-plan")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("Provision failed: %d", rr.Code)
	}

	// Step 2: LastOperation while provisioning (task processing)
	req := httptest.NewRequest("GET", fmt.Sprintf("/v2/service_instances/%s/last_operation", instanceID), nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	var loResp LastOperationResponse
	json.Unmarshal(rr.Body.Bytes(), &loResp)
	if loResp.State != "in progress" {
		t.Errorf("Step 2: state = %q, want %q", loResp.State, "in progress")
	}

	// Step 3: BOSH task completes
	taskState = "done"
	req = httptest.NewRequest("GET", fmt.Sprintf("/v2/service_instances/%s/last_operation", instanceID), nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	json.Unmarshal(rr.Body.Bytes(), &loResp)
	if loResp.State != "succeeded" {
		t.Errorf("Step 3: state = %q, want %q", loResp.State, "succeeded")
	}

	// Step 4: Bind
	bindBody := BindRequest{ServiceID: "openclaw-service", PlanID: "openclaw-developer-plan"}
	bodyBytes, _ := json.Marshal(bindBody)
	req = httptest.NewRequest("PUT", fmt.Sprintf("/v2/service_instances/%s/service_bindings/bind-001", instanceID), bytes.NewReader(bodyBytes))
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("Step 4: Bind failed: %d, body: %s", rr.Code, rr.Body.String())
	}

	// Step 5: Unbind
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/v2/service_instances/%s/service_bindings/bind-001", instanceID), nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Step 5: Unbind status = %d, want %d", rr.Code, http.StatusOK)
	}

	// Step 6: Deprovision
	taskState = "processing" // reset for deprovision
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/v2/service_instances/%s?service_id=openclaw-service&plan_id=openclaw-developer-plan", instanceID), nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("Step 6: Deprovision failed: %d", rr.Code)
	}

	// Step 7: LastOperation during deprovisioning
	req = httptest.NewRequest("GET", fmt.Sprintf("/v2/service_instances/%s/last_operation", instanceID), nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	json.Unmarshal(rr.Body.Bytes(), &loResp)
	if loResp.State != "in progress" {
		t.Errorf("Step 7: state = %q, want %q", loResp.State, "in progress")
	}

	// Step 8: Deprovision task completes
	taskState = "done"
	req = httptest.NewRequest("GET", fmt.Sprintf("/v2/service_instances/%s/last_operation", instanceID), nil)
	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	json.Unmarshal(rr.Body.Bytes(), &loResp)
	if loResp.State != "succeeded" {
		t.Errorf("Step 8: state = %q, want %q", loResp.State, "succeeded")
	}

	// Verify instance is deleted
	b.mu.RLock()
	_, exists := b.instances[instanceID]
	b.mu.RUnlock()
	if exists {
		t.Error("Instance should be deleted after deprovisioning completes")
	}
}
